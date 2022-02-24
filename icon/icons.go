package icon

import (
	"embed"
	"encoding/base64"
	"io/fs"
	"log"
)

//go:embed *.png
var files embed.FS

var (
	EncodedFiles map[string]string
	Filenames    []string
)

func init() {
	EncodedFiles = make(map[string]string)
	filenames, err := fs.Glob(files, "*.png")
	if err != nil {
		panic(err)
	}
	for _, filename := range filenames {
		data, err := fs.ReadFile(files, filename)
		if err != nil {
			log.Fatalf("%v: %v", filename, err)
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		Filenames = append(Filenames, filename)
		EncodedFiles[filename] = encoded
	}
}
