# sessionup-stormstore
This is an [Storm](https://github.com/asdine/storm) session store implementation for [sessionup](https://github.com/swithek/sessionup)

## Installation

To install simply use:

```
go get github.com/davseby/sessionup-stormstore
```

## Usage

To create and use new StormStore:

```go
// quick way of opening a storm database
db, err := storm.Open("my.db")
if err != nil {
      // handle error
}

store, err := stormstore.New(db)
if err != nil {
      // handle error
}

manager := sessionup.NewManager(store)
```