package ws

import (
	"log"
	"net/http"
)

func serveDefault(w http.ResponseWriter, r *http.Request) {
	log.Println(r.URL)
	if r.URL.Path != "/" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.ServeFile(w, r, "home.html")
}

func ServeWs() {
	go Hub.Run()
	// http.HandleFunc("/", serveDefault)
	http.HandleFunc("/ws", HandleWs)
	//Listerning on port :8080...
	log.Fatal(http.ListenAndServe(":8080", nil))
}
