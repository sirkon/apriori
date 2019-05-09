package modinfo

import (
	"github.com/sirkon/goproxy/gomod"
)

// NewChannelFromGoMod channel over go.mod dependencies (requirements and versioned replaces)
func NewChannelFromGoMod(src *gomod.Module) chan Result {
	res := make(chan Result)

	go func() {
		defer close(res)

		for mod, version := range src.Require {
			res <- Module{
				Path:    mod,
				Version: version,
			}
		}
		for _, item := range src.Replace {
			switch v := item.(type) {
			case gomod.Dependency:
				res <- Module{
					Path:    v.Path,
					Version: v.Version,
				}
			default:
			}
		}
	}()

	return res
}
