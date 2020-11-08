
# sessionup-boltstore
[![GoDoc](https://godoc.org/github.com/davseby/sessionup-boltstore?status.png)](https://godoc.org/github.com/davseby/sessionup-boltstore)
[![Test coverage](http://gocover.io/_badge/github.com/davseby/sessionup-boltstore)](https://gocover.io/github.com/davseby/sessionup-boltstore)
[![Go Report Card](https://goreportcard.com/badge/github.com/davseby/sessionup-boltstore)](https://goreportcard.com/report/github.com/davseby/sessionup-boltstore)

This is an [Bolt](https://github.com/boltdb/bolt) session store implementation for [sessionup](https://github.com/swithek/sessionup)

## Installation

To install simply use:

```
go get github.com/davseby/sessionup-boltstore
```

## Usage

To create and use new BoltStore:

```go
// quick way of opening a bolt database
db, err := bolt.Open("my.db", 0600, nil)
if err != nil {
      // handle error
}

// bucket parameter is a bucket name in which you want your sessions
// to be stored and managed. Cannot be an empty string.
// cleanupInterval parameter is an interval time between each clean up. If
// this interval is equal to zero, cleanup won't be executed. Cannot be less than
// zero.
store, err := boltstore.New(db, "sessions", time.Minute)
if err != nil {
      // handle error
}

manager := sessionup.NewManager(store)
```

Don't forget to handle clean up errors by using it's CleanupErr channel:

```go
for {
      select {
            case err := <-store.CleanUpErr():
                  // handle err
      }
}
```

If you want to close auto clean up process simply use Close
It  will always returns nil as an error (used to implement io.Closer interface).

```go
store.Close()
```
