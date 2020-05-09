package main

import (
	"net/http"

	"github.com/gorilla/mux"
)

func (app *application) NewRouter() *mux.Router {

	router := mux.NewRouter().StrictSlash(true)
	//router.NotFoundHandler = http.HandlerFunc(webHandler.notFound)

	router.
		Methods("GET").
		Path("/ws").
		Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.wsHub.serveWs(w, r)
		}))

	router.
		Methods("GET").
		Path("/").
		Handler(http.HandlerFunc(serveRAW))

	//router.PathPrefix("/app/").Handler(http.StripPrefix("/app/", http.FileServer(http.Dir("./web/"))))
	//router.PathPrefix("/app").Handler(http.StripPrefix("/app", http.FileServer(http.Dir("./web/"))))

	return router
}
