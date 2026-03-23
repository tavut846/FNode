package core

import (
	"errors"

	"github.com/InazumaV/FNode/conf"
)

var (
	cores = map[string]func(c *conf.CoreConfig) (Core, error){}
)

func NewCore(c []conf.CoreConfig) (Core, error) {
	if len(c) == 0 {
		return nil, errors.New("no have vail core")
	}
	// Only the first core is used in FNode
	if f, ok := cores[c[0].Type]; ok {
		return f(&c[0])
	}
	// Fallback to sing core if type is unknown but sing is registered
	if f, ok := cores["sing"]; ok {
		return f(&c[0])
	}
	return nil, errors.New("unknown core type and sing core not registered")
}

func RegisterCore(t string, f func(c *conf.CoreConfig) (Core, error)) {
	cores[t] = f
}

func RegisteredCore() []string {
	cs := make([]string, 0, len(cores))
	for k := range cores {
		cs = append(cs, k)
	}
	return cs
}
