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
	db     *storm.DB
	bucket string
}

// New creates a returns a fresh intance of BBoltStore.
func New(db *bbolt.DB, bucket string) (*BBoltStore, error) {
	if bucket == "" {
		return nil, errors.New("invalid bucket name")
	}

	sdb, err := storm.Open("", storm.UseDB(db))
	if err != nil {
		return nil, err
	}

	return &BBoltStore{
		db:     sdb,
		bucket: bucket,
	}, nil
}

// Create inserts provided session into the store and ensures
// that it is deleted when expiration time is due.
func (bs *BBoltStore) Create(ctx context.Context, s sessionup.Session) error {
	b := bs.db.From(bs.bucket)

	r := record{}
	if err := b.One("ID", s.ID, &r); err != nil {
		if !errors.Is(err, storm.ErrNotFound) {
			// unlikely to happen
			return err
		}
	}

	if r.ID == s.ID {
		return sessionup.ErrDuplicateID
	}

	// add new record
	r = newRecord(s)
	if err := b.Save(&r); err != nil {
		// unlikely to happen
		return err
	}

	// schedule record for deletion
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(s.ExpiresAt)):
			_ = b.DeleteStruct(&r)
		}
	}()

	return nil
}

// FetchByID retrieves a session from the store by the provided ID.
// The second returned value indicates whether the session was found
// or not (true == found), error will be nil if session is not found.
func (bs *BBoltStore) FetchByID(_ context.Context, id string) (sessionup.Session, bool, error) {
	b := bs.db.From(bs.bucket)

	var r record
	if err := b.One("ID", id, &r); err != nil {
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
func (bs *BBoltStore) FetchByUserKey(_ context.Context, key string) ([]sessionup.Session, error) {
	b := bs.db.From(bs.bucket)

	var rr []record
	if err := b.Find("UserKey", key, &rr); err != nil {
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
func (bs *BBoltStore) DeleteByID(_ context.Context, id string) error {
	b := bs.db.From(bs.bucket)

	r := record{}
	if err := b.One("ID", id, &r); err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return nil
		}

		// unlikely to happen
		return err
	}

	if err := b.DeleteStruct(&r); err != nil {
		// unlikely to happen
		return err
	}

	return nil
}

// DeleteByUserKey deletes all sessions associated with the provided user key,
// except those whose IDs are provided as last argument.
// If none are found, this function will no-op.
func (bs *BBoltStore) DeleteByUserKey(_ context.Context, key string, expIDs ...string) error {
	b := bs.db.From(bs.bucket)

	var rr []*record
	if err := b.Find("UserKey", key, &rr); err != nil {
		if errors.Is(err, storm.ErrNotFound) {
			return nil
		}

		// unlikely to happen
		return err
	}

	tx, err := b.Begin(true)
	if err != nil {
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
