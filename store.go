package bboltstore

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/swithek/sessionup"
	"go.etcd.io/bbolt"
)

// BBoltStore is a bbolt implementation of sessionup.Store.
type BBoltStore struct {
	db        storm.Node
	errChan   chan error
	closeChan chan struct{}
}

// New creates a returns a fresh intance of BBoltStore.
// Second parameter is a bucket name in which you want your sessions
// to be stored and managed.
// Third parameter is an interval time between each clean up.
func New(db *bbolt.DB, bucket string, cleanUpInterval time.Duration) (*BBoltStore, error) {
	if bucket == "" {
		return nil, errors.New("invalid bucket name")
	}

	if cleanUpInterval < 0 {
		return nil, errors.New("invalid clean up interval")
	}

	sdb, err := storm.Open("", storm.UseDB(db))
	if err != nil {
		return nil, err
	}

	b := &BBoltStore{
		db:        sdb.From(bucket),
		errChan:   make(chan error),
		closeChan: make(chan struct{}),
	}

	go func() {
		for {
			select {
			case <-b.closeChan:
				return
			case <-time.After(cleanUpInterval):
				if err := b.cleanUp(); err != nil {
					// unlikely to happen
					b.errChan <- err
				}
			}
		}
	}()

	return b, nil
}

// Create inserts provided session into the store and ensures
// that it is deleted when expiration time is due.
func (b *BBoltStore) Create(_ context.Context, s sessionup.Session) error {
	r := record{}
	if err := b.db.One("ID", s.ID, &r); err != nil {
		if !errors.Is(err, storm.ErrNotFound) {
			// unlikely to happen
			return err
		}
	}

	if r.ID == s.ID {
		return sessionup.ErrDuplicateID
	}

	r = newRecord(s)
	if err := b.db.Save(&r); err != nil {
		// unlikely to happen
		return err
	}

	return nil
}

// FetchByID retrieves a session from the store by the provided ID.
// The second returned value indicates whether the session was found
// or not (true == found), error will be nil if session is not found.
func (b *BBoltStore) FetchByID(_ context.Context, id string) (sessionup.Session, bool, error) {
	var r record
	if err := b.db.One("ID", id, &r); err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return sessionup.Session{}, false, nil
		}

		// unlikely to happen
		return sessionup.Session{}, false, err
	}

	return r.extractSession(), true, nil
}

// FetchByUserKey retrieves all sessions associated with the
// provided user key. If none are found, both return values will be nil.
func (b *BBoltStore) FetchByUserKey(_ context.Context, key string) ([]sessionup.Session, error) {
	var rr []record
	if err := b.db.Find("UserKey", key, &rr); err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return nil, nil
		}

		// unlikely to happen
		return nil, err
	}

	ss := make([]sessionup.Session, len(rr))
	for i := range rr {
		ss[i] = rr[i].extractSession()
	}

	return ss, nil
}

// DeleteByID deletes the session from the store by the provided ID.
// If session is not found, this function will be no-op.
func (b *BBoltStore) DeleteByID(_ context.Context, id string) error {
	r := record{}
	if err := b.db.One("ID", id, &r); err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return nil
		}

		// unlikely to happen
		return err
	}

	if err := b.db.DeleteStruct(&r); err != nil {
		// unlikely to happen
		return err
	}

	return nil
}

// DeleteByUserKey deletes all sessions associated with the provided user key,
// except those whose IDs are provided as last argument.
// If none are found, this function will no-op.
func (b *BBoltStore) DeleteByUserKey(_ context.Context, key string, expIDs ...string) error {
	var rr []*record
	if err := b.db.Find("UserKey", key, &rr); err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return nil
		}

		// unlikely to happen
		return err
	}

	tx, err := b.db.Begin(true)
	if err != nil {
		// unlikely to happen
		return err
	}

	defer tx.Rollback() //nolint:errcheck // error checking is not needed.

Outer:
	for i := range rr {
		for id := range expIDs {
			if rr[i].ID == expIDs[id] {
				continue Outer
			}
		}

		if err := tx.DeleteStruct(rr[i]); err != nil {
			// unlikely to happen
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		// unlikely to happen
		return err
	}

	return nil
}

// CleanUpErr returns error channel.
func (b BBoltStore) CleanUpErr() <-chan error {
	return b.errChan
}

// Close closes BBoltStore.
func (b *BBoltStore) Close() error {
	close(b.errChan)
	close(b.closeChan)

	return nil
}

// cleanUp removes all expired records from the store.
func (b *BBoltStore) cleanUp() error {
	var rr []*record
	if err := b.db.All(&rr); err != nil {
		// unlikely to happen
		return err
	}

	n := time.Now()

	tx, err := b.db.Begin(true)
	if err != nil {
		// unlikely to happen
		return err
	}

	defer tx.Rollback() //nolint:errcheck // error checking is not needed.

	for i := range rr {
		if rr[i].ExpiresAt.Before(n) {
			if err := tx.DeleteStruct(rr[i]); err != nil {
				// unlikely to happen
				return err
			}
		}
	}

	if err := tx.Commit(); err != nil {
		// unlikely to happen
		return err
	}

	return nil
}

// record is used to store session data in bbolt store.
type record struct {
	// Current specifies whether this session's ID
	// matches the ID stored in the request's cookie or not.
	Current bool `json:"current"`

	// CreatedAt specifies a point in time when this session
	// was created.
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt specifies a point in time when this
	// session should become invalid and be deleted
	// from the store.
	ExpiresAt time.Time `json:"expires_at"`

	// ID specifies a unique ID used to find this session
	// in the store.
	ID string `json:"id" storm:"id"`

	// UserKey specifies a non-unique key used to find all
	// sessions of the same user.
	UserKey string `json:"user_key"`

	// IP specifies an IP address that was used to create
	// this session.
	IP net.IP `json:"ip"`

	// Agent specifies the User-Agent data that was used
	// to create this session.
	Agent struct {
		OS      string `json:"os"`
		Browser string `json:"browser"`
	} `json:"agent"`
}

// newRecord creates a fresh instance of new record.
func newRecord(s sessionup.Session) record {
	r := record{
		Current:   s.Current,
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		ID:        s.ID,
		UserKey:   s.UserKey,
		IP:        s.IP,
	}

	r.Agent.OS = s.Agent.OS
	r.Agent.Browser = s.Agent.Browser

	return r
}

// extractSession returns sessionup.Session data from the record.
func (r record) extractSession() sessionup.Session {
	s := sessionup.Session{
		Current:   r.Current,
		CreatedAt: r.CreatedAt,
		ExpiresAt: r.ExpiresAt,
		ID:        r.ID,
		UserKey:   r.UserKey,
		IP:        r.IP,
	}

	s.Agent.OS = r.Agent.OS
	s.Agent.Browser = r.Agent.Browser

	return s
}
