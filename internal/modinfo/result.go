package modinfo

// Result type safe scanning result
type Result interface {
	resultsUniqueMethod()
}

var _ error = Error{}
var _ Result = Error{}

// Error internal error
type Error struct {
	errMsg string
}

func (e Error) resultsUniqueMethod() {}
func (e Error) Error() string        { return e.errMsg }

var _ Result = Module{}

// Module of certain version
type Module struct {
	Path    string
	Version string
}

func (Module) resultsUniqueMethod() {}

var _ Result = Latest{}

// Latest module
type Latest struct {
	Path string
}

func (Latest) resultsUniqueMethod() {}
