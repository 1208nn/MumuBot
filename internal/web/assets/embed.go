package assets

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist/*
var embeddedFiles embed.FS

func fileSystem() fs.FS {
	sub, err := fs.Sub(embeddedFiles, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}

func Handler() http.Handler {
	return http.FileServer(http.FS(fileSystem()))
}
