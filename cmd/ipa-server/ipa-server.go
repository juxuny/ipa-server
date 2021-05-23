package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/google/uuid"
	"github.com/spf13/afero"

	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/iineva/ipa-server/cmd/ipa-server/service"
	"github.com/iineva/ipa-server/pkg/httpfs"
	"github.com/iineva/ipa-server/pkg/storager"
	"github.com/iineva/ipa-server/public"
)

func getEnv(key string, def ...string) string {
	v := os.Getenv(key)
	if v == "" && len(def) != 0 {
		v = def[0]
	}
	return v
}

func redirect(m map[string]string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := m[r.URL.Path]
		if ok {
			r.URL.Path = p
		}
		next.ServeHTTP(w, r)
	})
}

func main() {

	debug := flag.Bool("d", false, "enable debug logging")
	storageDir := flag.String("dir", "upload", "upload data storage dir")
	publicURL := flag.String("public-url", "", "server public url")
	metadataPath := flag.String("mata-path", "appList.json", "metadata storage path, use random secret path to keep your metadata safer")
	qiniuConfig := flag.String("qiniu", "", "qiniu config AK:SK:[ZONE]:BUCKET")
	qiniuURL := flag.String("qiniu-url", "", "qiniu public url, https://cdn.example.com")
	flag.Usage = usage
	flag.Parse()

	host := fmt.Sprintf("%s:%s", getEnv("ADDRESS", "0.0.0.0"), getEnv("PORT", "8080"))

	serve := http.NewServeMux()

	logger := log.NewLogfmtLogger(os.Stderr)
	logger = log.With(logger, "ts", log.TimestampFormat(time.Now, "2006-01-02 15:04:05.000"), "caller", log.DefaultCaller)

	var store storager.Storager
	if *qiniuConfig != "" {
		args := strings.Split(*qiniuConfig, ":")
		s, err := storager.NewQiniuStorager(args[0], args[1], args[2], args[3], *qiniuURL)
		if err != nil {
			panic(err)
		}
		store = s
	} else {
		store = storager.NewOsFileStorager(*storageDir)
	}

	srv := service.New(store, *publicURL, *metadataPath)
	listHandler := httptransport.NewServer(
		service.LoggingMiddleware(logger, "/api/list", *debug)(service.MakeListEndpoint(srv)),
		service.DecodeListRequest,
		service.EncodeJsonResponse,
	)
	findHandler := httptransport.NewServer(
		service.LoggingMiddleware(logger, "/api/info", *debug)(service.MakeFindEndpoint(srv)),
		service.DecodeFindRequest,
		service.EncodeJsonResponse,
	)
	addHandler := httptransport.NewServer(
		service.LoggingMiddleware(logger, "/api/upload", *debug)(service.MakeAddEndpoint(srv)),
		service.DecodeAddRequest,
		service.EncodeJsonResponse,
	)
	deleteHandler := httptransport.NewServer(
		service.LoggingMiddleware(logger, "/api/delete", *debug)(service.MakeDeleteEndpoint(srv)),
		service.DecodeDeleteRequest,
		service.EncodeJsonResponse,
	)
	plistHandler := httptransport.NewServer(
		service.LoggingMiddleware(logger, "/plist", *debug)(service.MakePlistEndpoint(srv)),
		service.DecodePlistRequest,
		service.EncodePlistResponse,
	)

	// parser API
	serve.Handle("/api/list", listHandler)
	serve.Handle("/api/info/", findHandler)
	serve.Handle("/api/upload", addHandler)
	serve.Handle("/api/delete", deleteHandler)
	serve.Handle("/plist/", plistHandler)

	// static files
	uploadFS := afero.NewBasePathFs(afero.NewOsFs(), *storageDir)
	staticFS := httpfs.New(
		http.FS(public.FS),
		httpfs.NewAferoFS(uploadFS),
	)
	serve.Handle("/", redirect(map[string]string{
		"/key": "/key.html",
		// random path to block local metadata
		fmt.Sprintf("/%s", *metadataPath): fmt.Sprintf("/%s", uuid.NewString()),
	}, http.FileServer(staticFS)))

	logger.Log("msg", fmt.Sprintf("SERVER LISTEN ON: http://%v", host))
	logger.Log("msg", http.ListenAndServe(host, serve))
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: ipa-server [options]
Options:
`)
	flag.PrintDefaults()
}
