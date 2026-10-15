package main

import "net/http"

// Retire the entire legacy anonymous download surface, including old files that
// remain on disk. Operations owns archival/removal; the Hub never serves them.
func registerRetiredProvisionDownloads(mux *http.ServeMux) {
	mux.HandleFunc("/dl", provisionDownload)
	mux.HandleFunc("/dl/", provisionDownload)
}

func provisionDownload(w http.ResponseWriter, r *http.Request) {
	http.NotFound(w, r)
}
