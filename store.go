package boltstore

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/asdine/storm/v3/q"

	"github.com/asdine/storm/v3"
	"github.com/swithek/sessionup"
	bolt "go.etcd.io/bbolt"
)

var (
	// ErrInvalidBucket is returned when invalid bucket name is provided.
	ErrInvalidBucket = errors.New("invalid bucket name")

	// ErrInvalidInterval is returned when invalid cleanup interval is provided.
	ErrInvalidInterval = errors.New("invalid cleanup interval")
)

// BoltStore is a bolt implementation of sessionup.Store.
type BoltStore struct {
	db      storm.Node
	errCh   chan error
	closeCh chan struct{}
}

// New creates and returns a fresh intance of BoltStore.
// Bucket parameter determines bucket name which is used to store and manage
// sessions, it cannot be an empty string.
// Cleanup interval parameter is an interval time between each clean up. If
// this interval is equal to zero, cleanup won't be executed. Cannot be less than
// zero.
func New(db *bolt.DB, bucket string, cleanupInterval time.Duration) (*BoltStore, error) {
	if bucket == "" {
		return nil, ErrInvalidBucket
	}

	if cleanupInterval < 0 {
		return nil, ErrInvalidInterval
	}

	sdb, err := storm.Open("", storm.UseDB(db))
	if err != nil {
		return nil, err
	}

	b := &BoltStore{
		db:      sdb.From(bucket),
		errCh:   make(chan error),
		closeCh: make(chan struct{}),
	}

	if cleanupInterval != 0 {
		go func() {
			t := time.NewTicker(cleanupInterval)
			defer t.Stop()
			for {
				select {
				case <-b.closeCh:
					return
				case <-t.C:
					if err := b.cleanup(); err != nil {
						// unlikely to happen
						b.errCh <- err
					}
				}
			}
		}()
	}

	return b, nil
}

// Create inserts provided session into the store and ensures
// that it is deleted when expiration time is due.
func (b *BoltStore) Create(_ context.Context, s sessionup.Session) error {
	var r record
	if err := b.detectErr(b.db.One("ID", s.ID, &r)); err != nil {
		// unlikely to happen
		return err
	}

	if r.ID == s.ID {
		return sessionup.ErrDuplicateID
	}

	r = newRecord(s)

	return b.detectErr(b.db.Save(&r))
}

// FetchByID retrieves a session from the store by the provided ID.
// The second returned value indicates whether the session was found
// or not (true == found), error will be nil if session is not found.
func (b *BoltStore) FetchByID(_ context.Context, id string) (sessionup.Session, bool, error) {
	var r record
	if err := b.db.One("ID", id, &r); err != nil {
		return sessionup.Session{}, false, b.detectErr(err)
	}

	return r.extractSession(), true, nil
}

// FetchByUserKey retrieves all sessions associated with the
// provided user key. If none are found, both return values will be nil.
func (b *BoltStore) FetchByUserKey(_ context.Context, key string) ([]sessionup.Session, error) {
	var rr []record
	if err := b.db.Find("UserKey", key, &rr); err != nil {
		return nil, b.detectErr(err)
	}

	ss := make([]sessionup.Session, len(rr))
	for i := range rr {
		ss[i] = rr[i].extractSession()
	}

	return ss, nil
}

// DeleteByID deletes the session from the store by the provided ID.
// If session is not found, this function will be no-op.
func (b *BoltStore) DeleteByID(_ context.Context, id string) error {
	return b.detectErr(b.db.Select(
		q.Eq("ID", id),
	).Delete(&record{}))
}

// DeleteByUserKey deletes all sessions associated with the provided user key,
// except those whose IDs are provided as last argument.
// If none are found, this function will no-op.
func (b *BoltStore) DeleteByUserKey(_ context.Context, key string, expIDs ...string) error {
	return b.detectErr(b.db.Select(
		q.Eq("UserKey", key),
		q.Not(
			q.In("ID", expIDs),
		),
	).Delete(&record{}))
}

// CleanupErr returns a channel that should be used to read and handle errors
// that occured during cleanup process. Whenever the cleanup service is active,
// errors from this channel will have to be drained, otherwise cleanup won't be able
// to continue its process.
func (b BoltStore) CleanupErr() <-chan error {
	return b.errCh
}

// Close stops the cleanup service.
// It always returns nil as an error, used to implement io.Closer interface.
func (b *BoltStore) Close() error {
	b.closeCh <- struct{}{}
	close(b.closeCh)
	close(b.errCh)

	return nil
}

// cleanup removes all expired records from the store by their expiration time.
func (b *BoltStore) cleanup() error {
	return b.detectErr(b.db.Select(
		q.Lte("ExpiresAt", time.Now()),
	).Delete(&record{}))
}

// detectError is a helper that transforms errors.
func (b BoltStore) detectErr(err error) error {
	if errors.Is(err, storm.ErrNotFound) {
		return nil
	}

	return err
}

// record is used to store session data in bolt store.
type record struct {
	// ID specifies a unique ID used to find this session
	// in the store.
	ID string `json:"id"`

	// CreatedAt specifies a point in time when this session
	// was created.
	CreatedAt time.Time `json:"created_at"`

	// ExpiresAt specifies a point in time when this
	// session should become invalid and be deleted
	// from the store.
	ExpiresAt time.Time `json:"expires_at"`

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
