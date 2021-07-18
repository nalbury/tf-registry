package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jszwec/s3fs"
)

// ModuleBasePath is the base v1 api path for the terraform registry
const ModuleBasePath = "/terraform/modules/v1"

// ServiceDiscoveryResp is our service discovery response struct
type ServiceDiscoveryResp struct {
	ModulesV1 string `json:"modules.v1"`
}

// Module versions is a list of module version maps
type ModuleVersions struct {
	Versions []map[string]string `json:"versions"`
}

// ModuleVersionsResp is our module versions response struct
type ModuleVersionsResp struct {
	Modules []ModuleVersions `json:"modules"`
}

// Module respresents a terraform module
type Module struct {
	Namespace string
	Name      string
	Provider  string
	Version   string
}

// getModuleVersions is a helper function to look up all versions for a module
func getModuleVersions(modPath string) (ModuleVersionsResp, error) {
	m := ModuleVersions{}
	versionDirs, err := fs.ReadDir(s3fsys, modPath)
	if err != nil {
		return ModuleVersionsResp{}, err
	}
	for _, v := range versionDirs {
		vers := map[string]string{"version": v.Name()}
		m.Versions = append(m.Versions, vers)
	}
	return ModuleVersionsResp{
		Modules: []ModuleVersions{m},
	}, nil
}

///////////////////
// HTTP HANDLERS //
///////////////////

// httpGetServiceDiscovery is a http handler for returning the
// base path for the modules API provided by this registry
func httpGetServiceDiscovery(w http.ResponseWriter, r *http.Request) {
	// Service discovery resp
	s := ServiceDiscoveryResp{ModulesV1: ModuleBasePath}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

// httpGetVersions is a http handler for retrieving a list of module versions
// the registry server expects the versions to all be a set of
// sub-directories in our fs.FS backend (s3), rooted at the module's base path:
//   {registry_namespace}/{module_name}/{provider_name}/1.0.0/
//   {registry_namespace}/{module_name}/{provider_name}/2.0.0/
func httpGetVersions(w http.ResponseWriter, r *http.Request) {
	m := Module{
		Namespace: chi.URLParam(r, "namespace"),
		Name:      chi.URLParam(r, "name"),
		Provider:  chi.URLParam(r, "provider"),
	}
	modPath := filepath.Join(prefix, m.Namespace, m.Name, m.Provider)
	modVers, err := getModuleVersions(modPath)
	if err != nil {
		// TODO handle module not found with 404
		http.Error(w, err.Error(), 500)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(modVers)
}

// httpGetDownLoadURL is a http handler for retrieving the final download URL for a terraform module,
// the terraform client expects an empty response (204),
// the download URL is set in the header X-Terraform-Get
func httpGetDownloadURL(w http.ResponseWriter, r *http.Request) {
	m := Module{
		Namespace: chi.URLParam(r, "namespace"),
		Name:      chi.URLParam(r, "name"),
		Provider:  chi.URLParam(r, "provider"),
		Version:   chi.URLParam(r, "version"),
	}
	tfGetHeader := filepath.Join(
		"/download",
		m.Namespace,
		m.Name,
		m.Provider,
		m.Version,
		m.Name+".tgz",
	)
	w.Header().Set("X-Terraform-Get", tfGetHeader)
	w.WriteHeader(http.StatusNoContent)
}

// httpGetModule is a http handler for retrieving a terraform module
// we use an s3 based implementation of go's fs.FS interface,
// which is compatible with the built in http.FilServer
func httpGetModule(w http.ResponseWriter, r *http.Request) {
	// Force Content-* headers that terraform client expects
	w.Header().Set("Content-Encoding", "application/octet-stream")
	w.Header().Set("Content-Type", "application/x-gzip")
	fs := http.StripPrefix("/download/", http.FileServer(http.FS(s3fsys)))
	fs.ServeHTTP(w, r)
}

// Globals
var (
	bucket  string
	profile string
	prefix  string
	port    string
	s3fsys  fs.FS
)

func init() {
	flag.StringVar(&bucket, "bucket", "", "aws s3 bucket name containing terraform modules")
	flag.StringVar(&profile, "profile", "default", "aws named profile to assume")
	flag.StringVar(&prefix, "prefix", "", "optional path prefix for modules in s3")
	flag.StringVar(&port, "port", "3000", "port for HTTP server")
}
func usage() {
	fmt.Fprint(flag.CommandLine.Output(), "Terraform Registry Server\n\n")
	fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] \n\nFlags:\n", os.Args[0])
	flag.PrintDefaults()
}

// TF Registry Server
func main() {
	flag.Usage = usage
	// Parse flags and args
	flag.Parse()

	// Make sure we have a bucketname set
	if bucket == "" {
		fmt.Printf("bucket name not set!!!\n\n")
		usage()
		os.Exit(1)
	}

	fmt.Printf("Starting tf-registry webserver on 0.0.0.0:%s...\n", port)
	fmt.Printf("Connecting to storage backend...\n")

	// Create an AWS client session
	sessionOptions := session.Options{
		Profile:                 profile,
		SharedConfigState:       session.SharedConfigEnable,
		AssumeRoleTokenProvider: stscreds.StdinTokenProvider,
	}
	sess, err := session.NewSessionWithOptions(sessionOptions)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	// Create an fs.FS interface for our s3 bucket
	// TODO the implementation of fs.FS we're importing here is functional,
	// but its a simple pkg and would be neat to implement directly.
	// Would also allow for additional backend options (google cloud, azure, local fs etc.)
	s3fsys = s3fs.New(s3.New(sess), bucket)
	bucketRoot := filepath.Join(".")
	_, err = fs.Stat(s3fsys, bucketRoot)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Printf("Connection successful, serving terraform registry from: s3://%s/%s\n", bucket, prefix)

	// Configure a go-chi router
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.GetHead)
	// TODO implement a real healthcheck here
	r.Use(middleware.Heartbeat("/is_alive"))

	////////////
	// ROUTES //
	////////////

	// TODO group all routes below under go-chi r.Route structs where possible. Allows us to DRY up some of the headers etc.

	// GET / returns our static service discovery resp
	r.Get("/", httpGetServiceDiscovery)
	// GET /.well-known/terraform.json returns our static service discovery resp
	r.Get("/.well-known/terraform.json", httpGetServiceDiscovery)

	// GET /:namespace/:name/:provider/versions returns a list of versions for the specified module path
	r.Get(ModuleBasePath+"/{namespace}/{name}/{provider}/versions", httpGetVersions)
	// GET /:namespace/:name/:provider/:version/download responds with a 204 and X-Terraform-Get header pointing to the download path
	r.Get(ModuleBasePath+"/{namespace}/{name}/{provider}/{version}/download", httpGetDownloadURL)

	// GET /download/ provides an http fileserver for downloading modules as gzipped tarballs
	r.Get("/download/*", httpGetModule)

	// Run http server
	http.ListenAndServe(":"+port, r)
}
