
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

To create and use new BoltStore use `New` method. It accepts three parameters, the
first one being an open bolt database. Then a bucket name is required in which sessions 
will be stored and managed. The bucket parameter cannot be an empty string. Lastly 
it is required that you specify the duration between cleanup intervals, if the provided
value is zero the cleanup process won't be started. It cannot be less than zero.

```go
db, err := bolt.Open("my.db", 0600, nil)
if err != nil {
      // handle error
}

store, err := boltstore.New(db, "sessions", time.Minute)
if err != nil {
      // handle error
}

manager := sessionup.NewManager(store)
```

Don't forget to handle cleanup errors by using CleanupErr method. Channel should 
be used only for receiving errors. Whenever the cleanup service is active, errors 
from this channel will have to be drained, otherwise cleanup won't be able to 
continue its process.

```go
for {
      select {
            case err := <-store.CleanupErr():
                  // handle err
      }
}
```

If you want to close auto cleanup process simply use `Close` method. It won't close the
database so you will still be able to use all of the methods described by 
the sessionup store interface.
It will always return nil as an error, implements io.Closer interface.

```go
store.Close()
```
