# sessionup-bboltstore
This is an [BBolt](https://github.com/boltdb/bolt) session store implementation for [sessionup](https://github.com/swithek/sessionup)

## Installation

To install simply use:

```
go get github.com/davseby/sessionup-bboltstore
```

## Usage

To create and use new BBoltStore:

```go
// quick way of opening a bbolt database
db, err := bbolt.Open("my.db", 0600, nil)
if err != nil {
      // handle error
}

// second parameter is a bucket name in which you want your sessions
// to be stored and managed.
// third parameter is an interval time between each clean up.
store, err := bboltstore.New(db, "sessions", time.Minute)
if err != nil {
      // handle error
}

manager := sessionup.NewManager(store)
```

Don't forget to handle clean up errors by using it's CleanUpErr channel:

```go
if err := <-store.CleanUpErr(); err != nil {
      // handle error
}
```

If you want to close auto clean up process simply use Close:
It always returns nil as an error, it is used to implement io.Closer interface.
```go
store.Close()
```