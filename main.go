package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/handlers"
)

type application struct {
	infoLog     *log.Logger
	warnLog     *log.Logger
	errorLog    *log.Logger
	wsHub       *hub
	userAppPath string
}

func main() {
	app := &application{
		infoLog:     log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime),
		warnLog:     log.New(os.Stdout, "WARN\t", log.Ldate|log.Ltime),
		errorLog:    log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime),
		wsHub:       newHub(false),
		userAppPath: "/config/HassiGo",
	}

	app.infoLog.Printf("StartingHassiGo..")

	if _, err := os.Stat(app.userAppPath); os.IsNotExist(err) {
		os.MkdirAll(app.userAppPath, os.ModeDir)
	}

	go app.wsHub.run()

	ctx, done := context.WithCancel(context.Background())
	defer done()

	sigs := make(chan os.Signal, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigs:
			log.Println("Received the TERM or INTERUPT signal. Quitting")
			done()
			return
		}
	}()

	router := app.NewRouter()

	httpServer, httpErrorChan := startHTTPServer(":7080", router)

	httpShutdownCtx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	defer httpServer.Shutdown(httpShutdownCtx)

	go app.startWatcher(ctx)

	for {
		select {
		case err := <-httpErrorChan:
			app.errorLog.Printf("%v", err)
		case <-ctx.Done():
			return
		}
	}
}

func (app *application) startWatcher(ctx context.Context) {

	watcher, err := fsnotify.NewWatcher()
	defer watcher.Close()

	if err != nil {
		app.errorLog.Printf("Could not start file watcher: %v", err)
		return
	}

	if err := filepath.Walk(app.userAppPath, func(path string, fi os.FileInfo, err error) error {
		if fi.Name() == "hassigo-userapp" || fi.Name() == "hassigo-userapp-run" {
			return nil
		}

		if err != nil {
			fmt.Printf("Watcher error: %v", err)
			return nil
		}

		if fi.Mode().IsDir() {
			fmt.Printf("Adding: %s\n", path)
			return watcher.Add(path)
		}

		return nil
	}); err != nil {
		fmt.Println("ERROR", err)
	}

	userAppContext, userAppDone := context.WithCancel(context.Background())
	defer userAppDone()
	_ = userAppContext

	go func() {
		err := app.compileUserApp()

		if err != nil {
			app.errorLog.Printf("Compilation error: %v", err)
		}

		//app.wsHub.broadcast <- []byte("Test")
		err = app.runUserApp(ctx)
		if err != nil {
			app.errorLog.Printf("Run error: %v", err)
			return
		}

		app.infoLog.Printf("User app stopped")
	}()

	for {

		select {
		case event := <-watcher.Events:

			fmt.Printf("EVENT: %#v\n", event)

			err := app.compileUserApp()

			if err != nil {
				app.errorLog.Printf("Compilation error: %v", err)
				return
			}

			userAppDone()
			userAppContext, userAppDone = context.WithCancel(context.Background())
			defer userAppDone()

			go func() {
				//app.wsHub.broadcast <- []byte("Test")
				err = app.runUserApp(ctx)
				if err != nil {
					app.errorLog.Printf("Run error: %v", err)
					return
				}

				app.infoLog.Printf("User app stopped")
			}()
		case err := <-watcher.Errors:
			fmt.Println("ERROR", err)

		}
	}
}

func (app *application) compileUserApp() error {
	app.infoLog.Printf("Compiling user application...")

	cmd := exec.Command("go", "get")

	cmd.Dir = app.userAppPath

	mw := io.MultiWriter(os.Stdout, app.wsHub)

	cmd.Stdout = mw
	cmd.Stderr = mw

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("running go get failed with %s", err)
	}

	cmd = exec.Command("go", "build", "-o", "hassigo-userapp")

	cmd.Dir = app.userAppPath

	cmd.Stdout = mw
	cmd.Stderr = mw

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("compiling user app failed with %s", err)
	}

	return nil
}

func (app *application) runUserApp(ctx context.Context) error {
	app.infoLog.Printf("Running user application...")

	if _, err := os.Stat(filepath.Join(app.userAppPath, "hassigo-userapp")); !os.IsNotExist(err) {
		file1, err := os.Open(filepath.Join(app.userAppPath, "hassigo-userapp"))
		if err != nil {
			fmt.Println(err)
		}
		defer file1.Close()

		file2, err := os.Create(filepath.Join(app.userAppPath, "hassigo-userapp-run"))
		if err != nil {
			fmt.Println(err)
		}
		defer file2.Close()

		_, err = io.Copy(file2, file1)
		if err != nil {
			fmt.Println(err)
		}

		err = file2.Sync()
		if err != nil {
			fmt.Println(err)
		}
	}

	cmd := exec.Command("hassigo-user-app-run")

	cmd.Dir = app.userAppPath

	mw := io.MultiWriter(os.Stdout, app.wsHub)

	cmd.Stdout = mw
	cmd.Stderr = mw

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("running user app failed with %s", err)
	}

	for {
		select {
		case <-ctx.Done():
			cmd.Process.Kill()
			return nil
		}
	}

}

func startHTTPServer(listener string, handler http.Handler) (*http.Server, <-chan error) {
	/*cer, err := tls.LoadX509KeyPair("server.crt", "server.key")
	if err != nil {
		log.Println(err)
		return nil, nil
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cer}}
	*/
	srv := &http.Server{
		Addr:    listener,
		Handler: handler,
		//TLSConfig: tlsConfig,
	}
	errorChan := make(chan error)

	headersOk := handlers.AllowedHeaders([]string{"Content-Type", "X-Requested-With"})
	originsOk := handlers.AllowedOrigins([]string{"*"})
	methodsOk := handlers.AllowedMethods([]string{"GET", "HEAD", "POST", "PUT", "DELETE", "OPTIONS"})

	srv.Handler = handlers.CORS(headersOk, originsOk, methodsOk)(srv.Handler)

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			errorChan <- fmt.Errorf("http server error: %v", err)
		}
	}()

	// returning reference so caller can call Shutdown()
	return srv, errorChan
}
