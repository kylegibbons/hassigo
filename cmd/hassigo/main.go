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
	"strings"
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

type appEvent struct {
	appName string
	event   string
}

func main() {
	app := &application{
		infoLog:     log.New(os.Stdout, "INFO\t", log.Ldate|log.Ltime),
		warnLog:     log.New(os.Stdout, "WARN\t", log.Ldate|log.Ltime),
		errorLog:    log.New(os.Stdout, "ERROR\t", log.Ldate|log.Ltime),
		wsHub:       newHub(false),
		userAppPath: "/config/HassiGo",
	}

	app.infoLog.Printf("Starting HassiGo 0.0.29...")

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

	appEvents := make(chan appEvent, 10)

	err := app.compileUserApp()

	if err != nil {
		app.errorLog.Printf("Could not compile app: %v", err)
	}

	//userAppCpntext

	appEvents <- appEvent{
		appName: "test",
		event:   "new_version",
	}

	go app.startWatcher(ctx, appEvents)

	appChannels := make(map[string]chan appEvent)

	for {
		select {
		case event := <-appEvents:
			if _, ok := appChannels[event.appName]; !ok {
				appChannels[event.appName] = make(chan appEvent, 10)

				app.runUserApp(ctx, event.appName, appChannels[event.appName])
			}

			appChannels[event.appName] <- appEvent{
				appName: event.appName,
				event:   "restart",
			}

		case err := <-httpErrorChan:
			app.errorLog.Printf("%v", err)
		case <-ctx.Done():
			return
		}
	}
}

func (app *application) startWatcher(ctx context.Context, appChan chan appEvent) {

	watcher, err := fsnotify.NewWatcher()
	defer watcher.Close()

	if err != nil {
		app.errorLog.Printf("Could not start file watcher: %v", err)
		return
	}

	if err := filepath.Walk(app.userAppPath, func(path string, fi os.FileInfo, err error) error {

		if err != nil {
			app.errorLog.Printf("Watcher error: %v", err)
			return nil
		}

		if fi.Mode().IsDir() {
			app.infoLog.Printf("Adding: %s\n", path)
			return watcher.Add(path)
		}

		return nil
	}); err != nil {
		app.errorLog.Printf("Filepath walk error: %v", err)
	}

	for {

		select {
		case event := <-watcher.Events:
			if strings.Contains(event.Name, "userapp") {
				continue
			}

			app.infoLog.Printf("EVENT: %v\n", event)

			err := app.compileUserApp()

			if err != nil {
				app.errorLog.Printf("Compilation error: %v", err)
				continue
			}

			appChan <- appEvent{
				appName: "test",
				event:   "new_version",
			}

		case err := <-watcher.Errors:
			app.errorLog.Println("ERROR", err)
		case <-ctx.Done():
			return
		}
	}
}

func (app *application) compileUserApp() error {
	app.infoLog.Printf("Compiling user application...")

	cmd := exec.Command("go", "get")

	cmd.Dir = app.userAppPath

	mw := io.MultiWriter(os.Stdout, app.wsHub)

	cmd.Stdout = log.New(mw, "COMPILER:", 0).Writer()
	cmd.Stderr = log.New(mw, "COMPILER:", 0).Writer()

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("running go get failed with %s", err)
	}

	cmd = exec.Command("go", "build", "-o", "hassigo-userapp")

	cmd.Dir = app.userAppPath

	cmd.Stdout = log.New(mw, "COMPILER:", 0).Writer()
	cmd.Stderr = log.New(mw, "COMPILER:", 0).Writer()

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("compiling user app failed with %s", err)
	}

	return nil
}

func (app *application) runUserApp(ctx context.Context, name string, appChan chan appEvent) error {
	app.infoLog.Printf("Running user application...")

	err := app.copyUserApp()

	if err != nil {
		return err
	}

	mw := io.MultiWriter(os.Stdout, app.wsHub)

	go func() {
		cmd := exec.Command("./hassigo-userapp-run")

		cmd.Dir = app.userAppPath

		cmd.Stdout = log.New(mw, name, 0).Writer()
		cmd.Stderr = log.New(mw, name, 0).Writer()

		// app.infoLog.Printf("Starting app: %v", name)

		// err := cmd.Start()
		// if err != nil {
		// 	app.errorLog.Printf("running user app 1 failed with %s", err)
		// }

		for {
			select {
			case event := <-appChan:
				switch event.event {
				case "restart":
					if cmd.Process != nil {
						app.infoLog.Printf("Stopping app: %v", name)
						err := cmd.Process.Kill()

						if err != nil {
							app.errorLog.Printf("Could not kill user app: %s", err)
						}
					}

					app.infoLog.Println("Waiting for app to stop")

					cmd.Wait()

					app.infoLog.Println("Waiting for app to stop")

					err = app.copyUserApp()

					if err != nil {
						app.errorLog.Printf("Could not copy user app: %s", err)
					}

					app.infoLog.Printf("Starting app: %v", name)

					cmd = exec.Command("./hassigo-userapp-run")

					cmd.Dir = app.userAppPath

					cmd.Stdout = log.New(mw, name, 0).Writer()
					cmd.Stderr = log.New(mw, name, 0).Writer()

					err := cmd.Start()
					if err != nil {
						app.errorLog.Printf("Running user app failed with %s", err)
					}
				}
			case <-ctx.Done():
				if cmd.Process != nil {
					app.infoLog.Printf("Stopping app: %v", name)
					cmd.Process.Kill()
				}
				return
			}
		}
	}()

	return nil

}

func (app *application) copyUserApp() error {
	if _, err := os.Stat(filepath.Join(app.userAppPath, "hassigo-userapp")); !os.IsNotExist(err) {
		file1, err := os.Open(filepath.Join(app.userAppPath, "hassigo-userapp"))
		if err != nil {
			return err
		}
		defer file1.Close()

		file2, err := os.Create(filepath.Join(app.userAppPath, "hassigo-userapp-run"))
		if err != nil {
			return err
		}
		defer file2.Close()
		file2.Chmod(os.ModePerm)

		_, err = io.Copy(file2, file1)
		if err != nil {
			return err
		}

		err = file2.Sync()
		if err != nil {
			return err
		}
	}

	return nil
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
