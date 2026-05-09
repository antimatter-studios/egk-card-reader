package main

import "github.com/alecthomas/kong"

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("card-reader"),
		kong.Description(longDescription),
		kong.UsageOnError(),
	)
	kctx.FatalIfErrorf(kctx.Run())
}
