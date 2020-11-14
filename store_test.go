package boltstore

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/asdine/storm/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/swithek/sessionup"
	bolt "go.etcd.io/bbolt"
)

func Test_New(t *testing.T) {
	// invalid bucket
	s, err := New(&bolt.DB{}, "", time.Second)
	require.Equal(t, ErrInvalidBucket, err)
	assert.Nil(t, s)

	// invalid cleanup interval
	s, err = New(&bolt.DB{}, "ab", time.Second*-1)
	require.Equal(t, ErrInvalidInterval, err)
	assert.Nil(t, s)

	// invalid db
	s, err = New(&bolt.DB{}, "b", time.Second)
	require.Error(t, err)
	assert.Nil(t, s)

	// success
	db, err := bolt.Open(filepath.Join(t.TempDir(), "test.db"), 0601, nil)
	require.NoError(t, err)

	s, err = New(db, "b", 0)
	require.NoError(t, err)
	assert.NotNil(t, s)
	assert.NotNil(t, s.db)
	assert.NotNil(t, s.errCh)
	assert.NotNil(t, s.closeCh)
	assert.Equal(t, "0s", s.cleanupInterval.String())

	// auto cleanup doesn't delete old records
	r1 := stubRecord("ABC", "1", time.Now())
	require.NoError(t, s.db.Save(&r1))

	time.Sleep(time.Millisecond * 30)

	c, err := s.db.Count(&record{})
	require.NoError(t, err)
	assert.Equal(t, 1, c)

	// success
	db, err = bolt.Open(filepath.Join(t.TempDir(), "test2.db"), 0600, nil)
	require.NoError(t, err)

	s, err = New(db, "b", time.Millisecond*5)
	require.NoError(t, err)
	assert.NotNil(t, s)
	assert.NotNil(t, s.db)
	assert.NotNil(t, s.errCh)
	assert.NotNil(t, s.closeCh)
	assert.Equal(t, time.Millisecond*5, s.cleanupInterval)

	// auto cleanup deletes old records
	r2 := stubRecord("ABC", "1", time.Now())
	require.NoError(t, s.db.Save(&r2))

	assert.Eventually(t, func() bool {
		c, err = s.db.Count(&record{})
		require.NoError(t, err)

		return c == 0
	}, time.Second, time.Millisecond*5)

	// close stops auto cleanup process
	s.Close()

	r3 := stubRecord("ABC", "1", time.Now())
	require.NoError(t, s.db.Save(&r3))

	time.Sleep(time.Millisecond * 30)

	c, err = s.db.Count(&record{})
	require.NoError(t, err)
	assert.Equal(t, 1, c)

	// closed db error when trying to perform cleanup
	db, err = bolt.Open(filepath.Join(t.TempDir(), "test3.db"), 0600, nil)
	require.NoError(t, err)

	s, err = New(db, "c", time.Millisecond*5)
	require.NoError(t, err)
	assert.NotNil(t, s)
	assert.NotNil(t, s.db)
	assert.NotNil(t, s.errCh)
	assert.NotNil(t, s.closeCh)
	assert.Equal(t, time.Millisecond*5, s.cleanupInterval)

	ch := make(chan struct{})

	go func() {
		err := <-s.CleanupErr()
		assert.Error(t, err)

		close(ch)
	}()

	db.Close()

	<-ch
}

func Test_Store(t *testing.T) {
	suite.Run(t, &Suite{})
}

type Suite struct {
	suite.Suite

	tempDir string

	st *BoltStore
	db *bolt.DB
}

func (s *Suite) SetupSuite() {
	s.tempDir = s.T().TempDir()

	db, err := bolt.Open(filepath.Join(s.T().TempDir(), "test.db"), 0600, nil)
	s.Require().NoError(err)
	s.Require().NotNil(db)
	s.db = db
}

func (s *Suite) TearDownSuite() {
	s.Require().NoError(s.db.Close())
}

func (s *Suite) SetupTest() {
	db, err := storm.Open("", storm.UseDB(s.db))
	s.Require().NoError(err)

	s.st = &BoltStore{db: db}
}

func (s *Suite) TearDownTest() {
	err := s.st.db.Drop(&record{})
	if errors.Is(err, bolt.ErrBucketNotFound) {
		return
	}

	s.Require().NoError(err)
}

func (s *Suite) Test_BoltStore_Create() {
	// duplicate id
	r1 := stubRecord("ABC", "abc", time.Now())
	s.Require().NoError(s.st.db.Save(&r1))

	err := s.st.Create(context.Background(), r1.extractSession())
	s.Assert().Equal(sessionup.ErrDuplicateID, err)
	s.Require().NoError(s.st.db.DeleteStruct(&r1))

	// success
	s2 := stubSession("ABC", "2", time.Now())
	err = s.st.Create(context.Background(), s2)
	s.Assert().NoError(err)

	c, err := s.st.db.Count(&record{})
	s.Require().NoError(err)
	s.Require().Equal(1, c)

	// closed db
	db, err := bolt.Open(filepath.Join(s.T().TempDir(), "test.db"), 0600, nil)
	s.Require().NoError(err)
	s.Require().NotNil(db)

	sdb, err := storm.Open("", storm.UseDB(db))
	s.Require().NoError(err)

	bs := &BoltStore{db: sdb}

	db.Close()

	err = bs.Create(context.Background(), s2)
	s.Assert().Error(err)

}

func (s *Suite) Test_BoltStore_FetchByID() {
	// not found
	s1, ok, err := s.st.FetchByID(context.Background(), "3")
	s.Assert().Empty(s1)
	s.Assert().False(ok)
	s.Assert().NoError(err)

	// success
	r1 := stubRecord("ABC", "1", time.Now().Add(time.Millisecond*3))
	s.Require().NoError(s.st.db.Save(&r1))

	s1, ok, err = s.st.FetchByID(context.Background(), "1")
	s.Require().True(ok)
	s.Assert().NoError(err)
	equalSession(s.T(), r1.extractSession(), s1)

	// closed db
	db, err := bolt.Open(filepath.Join(s.T().TempDir(), "test.db"), 0600, nil)
	s.Require().NoError(err)
	s.Require().NotNil(db)

	sdb, err := storm.Open("", storm.UseDB(db))
	s.Require().NoError(err)

	bs := &BoltStore{db: sdb}

	db.Close()

	s2, ok, err := bs.FetchByID(context.Background(), "1")
	s.Require().Empty(s2)
	s.Require().False(ok)
	s.Assert().Error(err)
}

func (s *Suite) Test_BoltStore_FetchByUserKey() {
	// not found
	act, err := s.st.FetchByUserKey(context.Background(), "3")
	s.Assert().Nil(act)
	s.Assert().NoError(err)

	// success
	res := make([]sessionup.Session, 3)
	for i := range []int{0, 1, 2} {
		r := stubRecord("D", strconv.Itoa(i), time.Now())
		s.Require().NoError(s.st.db.Save(&r))
		res[i] = r.extractSession()
	}

	r := stubRecord("B", "4", time.Now())
	s.Require().NoError(s.st.db.Save(&r))

	act, err = s.st.FetchByUserKey(context.Background(), "D")
	s.Assert().NoError(err)
	s.Assert().Len(act, 3)

	for i := range res {
		equalSession(s.T(), res[i], act[i])
	}

	// closed db
	db, err := bolt.Open(filepath.Join(s.T().TempDir(), "test.db"), 0600, nil)
	s.Require().NoError(err)
	s.Require().NotNil(db)

	sdb, err := storm.Open("", storm.UseDB(db))
	s.Require().NoError(err)

	bs := &BoltStore{db: sdb}

	db.Close()

	act, err = bs.FetchByUserKey(context.Background(), "1")
	s.Require().Nil(act)
	s.Assert().Error(err)
}

func (s *Suite) Test_BoltStore_DeleteByID() {
	// not found
	err := s.st.DeleteByID(context.Background(), "3")
	s.Assert().NoError(err)

	// success
	var res []sessionup.Session
	for i := range []int{0, 1, 2} {
		r := stubRecord("D", strconv.Itoa(i), time.Now())
		s.Require().NoError(s.st.db.Save(&r))

		if i != 1 {
			res = append(res, r.extractSession())
		}
	}

	err = s.st.DeleteByID(context.Background(), "1")
	s.Assert().NoError(err)

	var act []*record
	s.Assert().NoError(s.st.db.All(&act))
	s.Assert().Len(act, 2)

	for i := range res {
		equalSession(s.T(), res[i], act[i].extractSession())
	}

	// closed db
	db, err := bolt.Open(filepath.Join(s.T().TempDir(), "test.db"), 0600, nil)
	s.Require().NoError(err)
	s.Require().NotNil(db)

	sdb, err := storm.Open("", storm.UseDB(db))
	s.Require().NoError(err)

	bs := &BoltStore{db: sdb}

	db.Close()

	err = bs.DeleteByID(context.Background(), "1")
	s.Assert().Error(err)
}

func (s *Suite) Test_BoltStore_DeleteByUserKey() {
	// not found
	err := s.st.DeleteByUserKey(context.Background(), "3")
	s.Assert().NoError(err)

	// success
	var res []sessionup.Session
	for i, k := range []string{"A", "D", "A", "C", "A", "A"} {
		r := stubRecord(k, strconv.Itoa(i), time.Now())
		s.Require().NoError(s.st.db.Save(&r))

		if k != "A" || i == 0 || i == 4 {
			res = append(res, r.extractSession())
		}
	}

	err = s.st.DeleteByUserKey(context.Background(), "A", "0", "4")
	s.Assert().NoError(err)

	var act []*record
	s.Assert().NoError(s.st.db.All(&act))
	s.Require().Len(act, 4)

	for i := range act {
		equalSession(s.T(), res[i], act[i].extractSession())
	}

	// closed db
	db, err := bolt.Open(filepath.Join(s.T().TempDir(), "test.db"), 0600, nil)
	s.Require().NoError(err)
	s.Require().NotNil(db)

	sdb, err := storm.Open("", storm.UseDB(db))
	s.Require().NoError(err)

	bs := &BoltStore{db: sdb}

	db.Close()

	err = bs.DeleteByUserKey(context.Background(), "1")
	s.Assert().Error(err)
}

func Test_BoltStore_CleanupErr(t *testing.T) {
	b := BoltStore{
		errCh: make(chan error),
	}

	err := errors.New("abc")

	go func() {
		b.errCh <- err
	}()

	time.Sleep(time.Millisecond * 3)

	assert.Equal(t, err, <-b.CleanupErr())
}

func (s *Suite) Test_BoltStore_cleanup() {
	// not found
	err := s.st.cleanup()
	s.Assert().NoError(err)

	// success
	var res []sessionup.Session
	for i, k := range []time.Duration{time.Second, time.Second * -1, time.Second} {
		r := stubRecord("A", strconv.Itoa(i), time.Now().Add(k))
		s.Require().NoError(s.st.db.Save(&r))

		if i != 1 {
			res = append(res, r.extractSession())
		}
	}

	err = s.st.cleanup()
	s.Assert().NoError(err)

	var act []*record
	s.Assert().NoError(s.st.db.All(&act))
	s.Require().Len(act, 2)

	for i := range act {
		equalSession(s.T(), res[i], act[i].extractSession())
	}

	// closed db
	db, err := bolt.Open(filepath.Join(s.T().TempDir(), "test.db"), 0600, nil)
	s.Require().NoError(err)
	s.Require().NotNil(db)

	sdb, err := storm.Open("", storm.UseDB(db))
	s.Require().NoError(err)

	bs := &BoltStore{db: sdb}

	db.Close()

	err = bs.cleanup()
	s.Assert().Error(err)
}

func Test_BoltStore_detectErr(t *testing.T) {
	assert.NoError(t, BoltStore{}.detectErr(storm.ErrNotFound))
	assert.NoError(t, BoltStore{}.detectErr(nil))
	assert.Equal(t, assert.AnError, BoltStore{}.detectErr(assert.AnError))
}

func Test_newRecord(t *testing.T) {
	n := time.Now()
	s := stubSession("123", "456", n)

	r := record{
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		ID:        s.ID,
		UserKey:   s.UserKey,
		IP:        s.IP,
		Meta:      s.Meta,
	}

	r.Agent.OS = s.Agent.OS
	r.Agent.Browser = s.Agent.Browser

	assert.Equal(t, r, newRecord(s))
}

func Test_record_extractSession(t *testing.T) {
	n := time.Now()
	s := stubSession("123", "456", n)

	r := record{
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
		ID:        s.ID,
		UserKey:   s.UserKey,
		IP:        s.IP,
		Meta:      s.Meta,
	}

	r.Agent.OS = s.Agent.OS
	r.Agent.Browser = s.Agent.Browser

	assert.Equal(t, s, r.extractSession())
}

func equalSession(t *testing.T, exp, act sessionup.Session) {
	assert.Equal(t, exp.UserKey, act.UserKey)
	assert.Equal(t, exp.ID, act.ID)
	assert.True(t, act.ExpiresAt.Equal(exp.ExpiresAt))
	assert.True(t, act.CreatedAt.Equal(exp.CreatedAt))
	assert.Equal(t, exp.Meta, act.Meta)
	assert.Equal(t, exp.Agent.OS, act.Agent.OS)
	assert.Equal(t, exp.Agent.Browser, act.Agent.Browser)
	if !reflect.DeepEqual(exp.IP, act.IP) {
		t.Errorf("want %v, got %v", exp.IP, act.IP)
	}
}

func stubSession(uk, id string, exp time.Time) sessionup.Session {
	ns := sessionup.Session{
		UserKey:   uk,
		ID:        id,
		ExpiresAt: exp,
		CreatedAt: time.Now(),
		IP:        net.ParseIP("127.0.0.1"),
		Meta: map[string]string{
			"123": "test",
		},
	}

	ns.Agent.OS = "gnu/linu"
	ns.Agent.Browser = "firefox"

	return ns
}

func stubRecord(uk, id string, exp time.Time) record {
	return newRecord(stubSession(uk, id, exp))
}
