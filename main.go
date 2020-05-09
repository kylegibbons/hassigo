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
	"runtime"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/handlers"
)

type application struct {
	infoLog  *log.Logger
	warnLog  *log.Logger
	errorLog *log.Logger
	wsHub    *hub
}

func main() {
	app := &application{
		infoLog:  log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime),
		warnLog:  log.New(os.Stdout, "WARN\t", log.Ldate|log.Ltime),
		errorLog: log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime),
		wsHub:    newHub(false),
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

	httpServer, httpErrorChan := startHTTPServer(":7070", router)

	httpShutdownCtx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	defer httpServer.Shutdown(httpShutdownCtx)

	go func() {
		watcher, err := fsnotify.NewWatcher()
		defer watcher.Close()

		if err != nil {
			app.errorLog.Printf("Could not start file watcher: %v", err)
			return
		}

		if err := filepath.Walk("..\\HomeAutomation\\hass\\", func(path string, fi os.FileInfo, err error) error {
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

		app.compileAndRunUserApp(userAppContext)

		for {
			userAppContext, userAppDone = context.WithCancel(context.Background())

			select {
			case event := <-watcher.Events:
				fmt.Printf("EVENT! %#v\n", event)
				userAppDone()
				app.compileAndRunUserApp(userAppContext)
			case err := <-watcher.Errors:
				fmt.Println("ERROR", err)

			}
		}
	}()

	for {
		select {
		case err := <-httpErrorChan:
			app.errorLog.Printf("%v", err)
		case <-ctx.Done():
			return
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

func (app *application) compileAndRunUserApp(ctx context.Context) {
	err := app.compileUserApp()

	if err != nil {
		app.errorLog.Printf("Compilation error: %v", err)
		return
	}

	//app.wsHub.broadcast <- []byte("Test")
	err = app.runUserApp(ctx)
	if err != nil {
		app.errorLog.Printf("Run error: %v", err)
		return
	}
}

func (app *application) compileUserApp() error {
	app.infoLog.Printf("Compiling user application...")

	cmd := exec.Command("go", "get")

	cmd.Dir = "..\\HomeAutomation\\hass"

	mw := io.MultiWriter(os.Stdout, app.wsHub)

	cmd.Stdout = mw
	cmd.Stderr = mw

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("running go get failed with %s", err)
	}

	cmd = exec.Command("go", "build", "-o", "hassigo-user-app.exe")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("go", "build", "-o", "hassigo-user-app.exe")
	}

	cmd.Dir = "..\\HomeAutomation\\hass"

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

	cmd := exec.Command("hassigo-user-app")

	if runtime.GOOS == "windows" {
		cmd = exec.Command(".\\hassigo-user-app.exe")
	}

	cmd.Dir = "..\\HomeAutomation\\hass"

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

	return nil

}
