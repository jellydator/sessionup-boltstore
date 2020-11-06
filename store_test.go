package stormstore

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
	"go.etcd.io/bbolt"
)

func Test_New(t *testing.T) {
	s := New(&storm.DB{}, "test")
	require.NotNil(t, s)
	assert.NotNil(t, s.db)
}

func Test_Store(t *testing.T) {
	suite.Run(t, &Suite{})
}

type Suite struct {
	suite.Suite

	tempDir string
	st      *StormStore
	db      *storm.DB
}

func (s *Suite) SetupSuite() {
	s.tempDir = s.T().TempDir()

	db, err := storm.Open(filepath.Join(s.tempDir, "test.db"))
	s.Require().NoError(err)
	s.Require().NotNil(db)
	s.db = db
}

func (s *Suite) TearDownSuite() {
	s.Require().NoError(s.db.Close())
}

func (s *Suite) SetupTest() {
	s.st = &StormStore{db: s.db}
}

func (s *Suite) TearDownTest() {
	err := s.st.db.Drop(&record{})
	if errors.Is(err, bbolt.ErrBucketNotFound) {
		return
	}

	s.Require().NoError(err)
}

func (s *Suite) Test_StormStore_Create() {
	// duplicate id
	r1 := stubRecord("ABC", "1", time.Now())
	s.Require().NoError(s.st.db.Save(&r1))

	s1 := stubSession("ABC", "1", time.Now())
	err := s.st.Create(context.Background(), s1)
	s.Assert().Equal(sessionup.ErrDuplicateID, err)
	s.Require().NoError(s.st.db.DeleteStruct(&r1))

	// success
	s2 := stubSession("ABC", "2", time.Now().Add(time.Millisecond*3))
	err = s.st.Create(context.Background(), s2)
	s.Assert().NoError(err)

	time.Sleep(time.Millisecond * 5)
	c, err := s.st.db.Count(&sessionup.Session{})
	s.Require().NoError(err)
	s.Require().Zero(c)

	// successful context cancelation
	ctx, cancel := context.WithCancel(context.Background())
	s3 := stubSession("ABC", "2", time.Now().Add(time.Millisecond*15))
	err = s.st.Create(ctx, s3)
	s.Assert().NoError(err)
	cancel()

	time.Sleep(time.Millisecond * 20)
	c, err = s.st.db.Count(&record{})
	s.Require().NoError(err)
	s.Require().Equal(1, c)
}

func (s *Suite) Test_StormStore_FetchByID() {
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
}

func (s *Suite) Test_StormStore_FetchByUserKey() {
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
}

func (s *Suite) Test_StormStore_DeleteByID() {
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
}

func (s *Suite) Test_StormStore_DeleteByUserKey() {
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
}

func Test_newRecord(t *testing.T) {
	n := time.Now()
	s := stubSession("123", "456", n)

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

	assert.Equal(t, r, newRecord(s))
}

func Test_record_extractSession(t *testing.T) {
	n := time.Now()
	s := stubSession("123", "456", n)

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

	assert.Equal(t, s, r.extractSession())
}

func equalSession(t *testing.T, exp, act sessionup.Session) {
	assert.Equal(t, exp.UserKey, act.UserKey)
	assert.Equal(t, exp.ID, act.ID)
	assert.True(t, act.ExpiresAt.Equal(exp.ExpiresAt))
	assert.True(t, act.CreatedAt.Equal(exp.CreatedAt))
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
	}

	ns.Agent.OS = "gnu/linu"
	ns.Agent.Browser = "firefox"

	return ns
}

func stubRecord(uk, id string, exp time.Time) record {
	return newRecord(stubSession(uk, id, exp))
}
