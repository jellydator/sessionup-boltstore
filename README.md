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
// quick way of opening a storm database
db, err := bbolt.Open("my.db", 0600, nil)
if err != nil {
      // handle error
}

store, err := bboltstore.New(db)
if err != nil {
      // handle error
}

manager := sessionup.NewManager(store)
```