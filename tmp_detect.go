package main

import (
	"fmt"
	"reflect"

	"github.com/sagernet/sing-box/option"
)

func main() {
	opts := option.V2RayTransportOptions{}
	typ := reflect.TypeOf(opts)
	for i := 0; i < typ.NumField(); i++ {
		fmt.Println(typ.Field(i).Name, typ.Field(i).Type.String())
	}
}
