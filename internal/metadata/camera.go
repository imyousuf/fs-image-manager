package metadata

import "strings"

// joinCamera builds the display/search camera string from the EXIF make and
// model. Camera models frequently already embed the make ("Canon EOS R5"), so we
// avoid the doubled "Canon Canon EOS R5": when the model starts with the make we
// return the model alone. Either field may be empty.
func joinCamera(make, model string) string {
	make = strings.TrimSpace(make)
	model = strings.TrimSpace(model)
	switch {
	case make == "" && model == "":
		return ""
	case make == "":
		return model
	case model == "":
		return make
	}
	// Case-insensitive prefix check so "NIKON CORPORATION" + "NIKON D850" still
	// collapses sensibly to the model.
	first := strings.Fields(make)[0]
	if strings.HasPrefix(strings.ToLower(model), strings.ToLower(first)) {
		return model
	}
	return make + " " + model
}
