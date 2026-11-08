package tmdb

import "strings"

// TMDB serves images from a path-only CDN: its API returns "/abc123.jpg" and
// the caller joins a base URL and a size. These are the sizes haaku stores.
const (
	// ImageSizePoster is the poster width haaku stores in images.poster.
	ImageSizePoster = "w500"
	// ImageSizeBackdrop is the backdrop width haaku stores in images.backdrop.
	ImageSizeBackdrop = "w1280"
	// ImageSizeStill is the episode-still width haaku stores.
	ImageSizeStill = "w300"
)

// imageBaseURL is TMDB's image CDN root. It is a documented constant rather
// than a GET /configuration call: the base does not vary per token, and a
// metadata fetch should not spend a request discovering it.
const imageBaseURL = "https://image.tmdb.org/t/p/"

// ImageURL joins a TMDB image path and size into an absolute URL. An empty
// path, or a value that is already absolute, is returned unchanged so callers
// can hand it either a TMDB path or an URL from another provider.
func ImageURL(path, size string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if size == "" {
		size = ImageSizePoster
	}
	return imageBaseURL + size + "/" + strings.TrimPrefix(path, "/")
}
