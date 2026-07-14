package main

import (
	"arduino/unoq"
	board "go.viam.com/rdk/components/board"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

func main() {
	// ModularMain can take multiple APIModel arguments, if your module implements multiple models.
	module.ModularMain(resource.APIModel{API: board.API, Model: unoq.UnoQ})
}
