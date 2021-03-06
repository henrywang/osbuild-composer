package worker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osbuild/osbuild-composer/internal/distro"
	"github.com/osbuild/osbuild-composer/internal/distro/test_distro"
	"github.com/osbuild/osbuild-composer/internal/jobqueue/fsjobqueue"
	"github.com/osbuild/osbuild-composer/internal/test"
	"github.com/osbuild/osbuild-composer/internal/worker"
)

func newTestServer(t *testing.T, tempdir string, identities []string) *worker.Server {
	q, err := fsjobqueue.New(tempdir)
	if err != nil {
		t.Fatalf("error creating fsjobqueue: %v", err)
	}
	return worker.NewServer(nil, q, "", identities)
}

// Ensure that the status request returns OK.
func TestStatus(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	server := newTestServer(t, tempdir, []string{})
	handler := server.Handler()
	test.TestRoute(t, handler, false, "GET", "/api/worker/v1/status", ``, http.StatusOK, `{"status":"OK"}`, "message")
}

func TestErrors(t *testing.T) {
	var cases = []struct {
		Method         string
		Path           string
		Body           string
		ExpectedStatus int
	}{
		// Bogus path
		{"GET", "/api/worker/v1/foo", ``, http.StatusNotFound},
		// Create job with invalid body
		{"POST", "/api/worker/v1/jobs", ``, http.StatusBadRequest},
		// Wrong method
		{"GET", "/api/worker/v1/jobs", ``, http.StatusMethodNotAllowed},
		// Update job with invalid ID
		{"PATCH", "/api/worker/v1/jobs/foo", `{"status":"FINISHED"}`, http.StatusBadRequest},
		// Update job that does not exist, with invalid body
		{"PATCH", "/api/worker/v1/jobs/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", ``, http.StatusBadRequest},
		// Update job that does not exist
		{"PATCH", "/api/worker/v1/jobs/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", `{"status":"FINISHED"}`, http.StatusNotFound},
	}

	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	for _, c := range cases {
		server := newTestServer(t, tempdir, []string{})
		handler := server.Handler()
		test.TestRoute(t, handler, false, c.Method, c.Path, c.Body, c.ExpectedStatus, "{}", "message")
	}
}

func TestCreate(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	distroStruct := test_distro.New()
	arch, err := distroStruct.GetArch(test_distro.TestArchName)
	if err != nil {
		t.Fatalf("error getting arch from distro: %v", err)
	}
	imageType, err := arch.GetImageType(test_distro.TestImageTypeName)
	if err != nil {
		t.Fatalf("error getting image type from arch: %v", err)
	}
	manifest, err := imageType.Manifest(nil, distro.ImageOptions{Size: imageType.Size(0)}, nil, nil, 0)
	if err != nil {
		t.Fatalf("error creating osbuild manifest: %v", err)
	}
	server := newTestServer(t, tempdir, []string{})
	handler := server.Handler()

	_, err = server.EnqueueOSBuild(arch.Name(), &worker.OSBuildJob{Manifest: manifest})
	require.NoError(t, err)

	test.TestRoute(t, handler, false, "POST", "/api/worker/v1/jobs",
		fmt.Sprintf(`{"types":["osbuild"],"arch":"%s"}`, test_distro.TestArchName), http.StatusCreated,
		`{"type":"osbuild","args":{"manifest":{"pipeline":{},"sources":{}}}}`, "id", "location", "artifact_location")
}

func TestCancel(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	distroStruct := test_distro.New()
	arch, err := distroStruct.GetArch(test_distro.TestArchName)
	if err != nil {
		t.Fatalf("error getting arch from distro: %v", err)
	}
	imageType, err := arch.GetImageType(test_distro.TestImageTypeName)
	if err != nil {
		t.Fatalf("error getting image type from arch: %v", err)
	}
	manifest, err := imageType.Manifest(nil, distro.ImageOptions{Size: imageType.Size(0)}, nil, nil, 0)
	if err != nil {
		t.Fatalf("error creating osbuild manifest: %v", err)
	}
	server := newTestServer(t, tempdir, []string{})
	handler := server.Handler()

	jobId, err := server.EnqueueOSBuild(arch.Name(), &worker.OSBuildJob{Manifest: manifest})
	require.NoError(t, err)

	token, j, typ, args, dynamicArgs, err := server.RequestJob(context.Background(), arch.Name(), []string{"osbuild"})
	require.NoError(t, err)
	require.Equal(t, jobId, j)
	require.Equal(t, "osbuild", typ)
	require.NotNil(t, args)
	require.Nil(t, dynamicArgs)

	test.TestRoute(t, handler, false, "GET", fmt.Sprintf("/api/worker/v1/jobs/%s", token), `{}`, http.StatusOK,
		`{"canceled":false}`)

	err = server.Cancel(jobId)
	require.NoError(t, err)

	test.TestRoute(t, handler, false, "GET", fmt.Sprintf("/api/worker/v1/jobs/%s", token), `{}`, http.StatusOK,
		`{"canceled":true}`)
}

func TestUpdate(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	distroStruct := test_distro.New()
	arch, err := distroStruct.GetArch(test_distro.TestArchName)
	if err != nil {
		t.Fatalf("error getting arch from distro: %v", err)
	}
	imageType, err := arch.GetImageType(test_distro.TestImageTypeName)
	if err != nil {
		t.Fatalf("error getting image type from arch: %v", err)
	}
	manifest, err := imageType.Manifest(nil, distro.ImageOptions{Size: imageType.Size(0)}, nil, nil, 0)
	if err != nil {
		t.Fatalf("error creating osbuild manifest: %v", err)
	}
	server := newTestServer(t, tempdir, []string{})
	handler := server.Handler()

	jobId, err := server.EnqueueOSBuild(arch.Name(), &worker.OSBuildJob{Manifest: manifest})
	require.NoError(t, err)

	token, j, typ, args, dynamicArgs, err := server.RequestJob(context.Background(), arch.Name(), []string{"osbuild"})
	require.NoError(t, err)
	require.Equal(t, jobId, j)
	require.Equal(t, "osbuild", typ)
	require.NotNil(t, args)
	require.Nil(t, dynamicArgs)

	test.TestRoute(t, handler, false, "PATCH", fmt.Sprintf("/api/worker/v1/jobs/%s", token), `{}`, http.StatusOK, `{}`)
	test.TestRoute(t, handler, false, "PATCH", fmt.Sprintf("/api/worker/v1/jobs/%s", token), `{}`, http.StatusNotFound, `*`)
}

func TestArgs(t *testing.T) {
	distroStruct := test_distro.New()
	arch, err := distroStruct.GetArch(test_distro.TestArchName)
	require.NoError(t, err)
	imageType, err := arch.GetImageType(test_distro.TestImageTypeName)
	require.NoError(t, err)
	manifest, err := imageType.Manifest(nil, distro.ImageOptions{Size: imageType.Size(0)}, nil, nil, 0)
	require.NoError(t, err)

	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	server := newTestServer(t, tempdir, []string{})

	job := worker.OSBuildJob{
		Manifest:  manifest,
		ImageName: "test-image",
	}
	jobId, err := server.EnqueueOSBuild(arch.Name(), &job)
	require.NoError(t, err)

	_, _, _, args, _, err := server.RequestJob(context.Background(), arch.Name(), []string{"osbuild"})
	require.NoError(t, err)
	require.NotNil(t, args)

	var jobArgs worker.OSBuildJob
	jobType, rawArgs, deps, err := server.Job(jobId, &jobArgs)
	require.NoError(t, err)
	require.Equal(t, args, rawArgs)
	require.Equal(t, job, jobArgs)
	require.Equal(t, jobType, "osbuild:"+arch.Name())
	require.Equal(t, []uuid.UUID(nil), deps)
}

func TestUpload(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	distroStruct := test_distro.New()
	arch, err := distroStruct.GetArch(test_distro.TestArchName)
	if err != nil {
		t.Fatalf("error getting arch from distro: %v", err)
	}
	imageType, err := arch.GetImageType(test_distro.TestImageTypeName)
	if err != nil {
		t.Fatalf("error getting image type from arch: %v", err)
	}
	manifest, err := imageType.Manifest(nil, distro.ImageOptions{Size: imageType.Size(0)}, nil, nil, 0)
	if err != nil {
		t.Fatalf("error creating osbuild manifest: %v", err)
	}
	server := newTestServer(t, tempdir, []string{})
	handler := server.Handler()

	jobID, err := server.EnqueueOSBuild(arch.Name(), &worker.OSBuildJob{Manifest: manifest})
	require.NoError(t, err)

	token, j, typ, args, dynamicArgs, err := server.RequestJob(context.Background(), arch.Name(), []string{"osbuild"})
	require.NoError(t, err)
	require.Equal(t, jobID, j)
	require.Equal(t, "osbuild", typ)
	require.NotNil(t, args)
	require.Nil(t, dynamicArgs)

	test.TestRoute(t, handler, false, "PUT", fmt.Sprintf("/api/worker/v1/jobs/%s/artifacts/foobar", token), `this is my artifact`, http.StatusOK, `?`)
}

func TestIdentities(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// distroStruct := test_distro.New()
	// arch, err := distroStruct.GetArch(test_distro.TestArchName)
	// require.NoError(t, err)
	// imageType, err := arch.GetImageType(test_distro.TestImageTypeName)
	// require.NoError(t, err)
	// manifest, err := imageType.Manifest(nil, distro.ImageOptions{Size: imageType.Size(0)}, nil, nil, 0)
	// require.NoError(t, err)

	server := newTestServer(t, tempdir, []string{"000000"})
	handler := server.Handler()

	// _, err := server.EnqueueOSBuild(arch.Name(), &worker.OSBuildJob{Manifest: manifest})
	// require.NoError(t, err)

	test.TestRoute(t, handler, false, "GET", "/api/worker/v1/status", ``, http.StatusNotFound, `{"message":"Auth header is not present"}`, "message")

	header := map[string]string{
		"x-rh-identity": "eyJlbnRpdGxlbWVudHMiOnsiaW5zaWdodHMiOnsiaXNfZW50aXRsZWQiOnRydWV9LCJzbWFydF9tYW5hZ2VtZW50Ijp7ImlzX2VudGl0bGVkIjp0cnVlfSwib3BlbnNoaWZ0Ijp7ImlzX2VudGl0bGVkIjp0cnVlfSwiaHlicmlkIjp7ImlzX2VudGl0bGVkIjp0cnVlfSwibWlncmF0aW9ucyI6eyJpc19lbnRpdGxlZCI6dHJ1ZX0sImFuc2libGUiOnsiaXNfZW50aXRsZWQiOnRydWV9fSwiaWRlbnRpdHkiOnsiYWNjb3VudF9udW1iZXIiOiIwMDAwMDMiLCJ0eXBlIjoiVXNlciIsInVzZXIiOnsidXNlcm5hbWUiOiJ1c2VyIiwiZW1haWwiOiJ1c2VyQHVzZXIudXNlciIsImZpcnN0X25hbWUiOiJ1c2VyIiwibGFzdF9uYW1lIjoidXNlciIsImlzX2FjdGl2ZSI6dHJ1ZSwiaXNfb3JnX2FkbWluIjp0cnVlLCJpc19pbnRlcm5hbCI6dHJ1ZSwibG9jYWxlIjoiZW4tVVMifSwiaW50ZXJuYWwiOnsib3JnX2lkIjoiMDAwMDAwIn19fQo=",
	}
	response := test.SendHTTPWithHeader(handler, "GET", "/api/worker/v1/status", ``, header)
	assert.Equal(t, 404, response.StatusCode, "status mismatch")

	header = map[string]string{
		"x-rh-identity": "eyJlbnRpdGxlbWVudHMiOnsiaW5zaWdodHMiOnsiaXNfZW50aXRsZWQiOnRydWV9LCJzbWFydF9tYW5hZ2VtZW50Ijp7ImlzX2VudGl0bGVkIjp0cnVlfSwib3BlbnNoaWZ0Ijp7ImlzX2VudGl0bGVkIjp0cnVlfSwiaHlicmlkIjp7ImlzX2VudGl0bGVkIjp0cnVlfSwibWlncmF0aW9ucyI6eyJpc19lbnRpdGxlZCI6dHJ1ZX0sImFuc2libGUiOnsiaXNfZW50aXRsZWQiOnRydWV9fSwiaWRlbnRpdHkiOnsiYWNjb3VudF9udW1iZXIiOiIwMDAwMDAiLCJ0eXBlIjoiVXNlciIsInVzZXIiOnsidXNlcm5hbWUiOiJ1c2VyIiwiZW1haWwiOiJ1c2VyQHVzZXIudXNlciIsImZpcnN0X25hbWUiOiJ1c2VyIiwibGFzdF9uYW1lIjoidXNlciIsImlzX2FjdGl2ZSI6dHJ1ZSwiaXNfb3JnX2FkbWluIjp0cnVlLCJpc19pbnRlcm5hbCI6dHJ1ZSwibG9jYWxlIjoiZW4tVVMifSwiaW50ZXJuYWwiOnsib3JnX2lkIjoiMDAwMDAwIn19fQ==",
	}

	response = test.SendHTTPWithHeader(handler, "GET", "/api/worker/v1/status", ``, header)
	assert.Equal(t, 200, response.StatusCode, "status mismatch")
}

func TestOAuth(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "worker-tests-")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	q, err := fsjobqueue.New(tempdir)
	require.NoError(t, err)
	workerServer := worker.NewServer(nil, q, tempdir, []string{"000000"})
	handler := workerServer.Handler()

	workSrv := httptest.NewServer(handler)
	defer workSrv.Close()

	/* Start a server which will act as a proxy, adding a valid identity header  */
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer accessToken!", r.Header.Get("Authorization"))
		r.Header.Set("x-rh-identity", "eyJlbnRpdGxlbWVudHMiOnsiaW5zaWdodHMiOnsiaXNfZW50aXRsZWQiOnRydWV9LCJzbWFydF9tYW5hZ2VtZW50Ijp7ImlzX2VudGl0bGVkIjp0cnVlfSwib3BlbnNoaWZ0Ijp7ImlzX2VudGl0bGVkIjp0cnVlfSwiaHlicmlkIjp7ImlzX2VudGl0bGVkIjp0cnVlfSwibWlncmF0aW9ucyI6eyJpc19lbnRpdGxlZCI6dHJ1ZX0sImFuc2libGUiOnsiaXNfZW50aXRsZWQiOnRydWV9fSwiaWRlbnRpdHkiOnsiYWNjb3VudF9udW1iZXIiOiIwMDAwMDAiLCJ0eXBlIjoiVXNlciIsInVzZXIiOnsidXNlcm5hbWUiOiJ1c2VyIiwiZW1haWwiOiJ1c2VyQHVzZXIudXNlciIsImZpcnN0X25hbWUiOiJ1c2VyIiwibGFzdF9uYW1lIjoidXNlciIsImlzX2FjdGl2ZSI6dHJ1ZSwiaXNfb3JnX2FkbWluIjp0cnVlLCJpc19pbnRlcm5hbCI6dHJ1ZSwibG9jYWxlIjoiZW4tVVMifSwiaW50ZXJuYWwiOnsib3JnX2lkIjoiMDAwMDAwIn19fQ==")
		handler.ServeHTTP(w, r)
	}))
	defer proxySrv.Close()

	offlineToken := "someOfflineToken"
	/* Start oauth srv supplying the bearer token */
	oauthSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		err = r.ParseForm()
		require.NoError(t, err)

		require.Equal(t, "refresh_token", r.FormValue("grant_type"))
		require.Equal(t, "rhsm-api", r.FormValue("client_id"))
		require.Equal(t, offlineToken, r.FormValue("refresh_token"))

		bt := struct {
			AccessToken     string `json:"access_token"`
			ValidForSeconds int    `json:"expires_in"`
		}{
			"accessToken!",
			900,
		}

		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(bt)
		require.NoError(t, err)
	}))
	defer oauthSrv.Close()

	distroStruct := test_distro.New()
	arch, err := distroStruct.GetArch(test_distro.TestArchName)
	if err != nil {
		t.Fatalf("error getting arch from distro: %v", err)
	}
	imageType, err := arch.GetImageType(test_distro.TestImageTypeName)
	if err != nil {
		t.Fatalf("error getting image type from arch: %v", err)
	}
	manifest, err := imageType.Manifest(nil, distro.ImageOptions{Size: imageType.Size(0)}, nil, nil, 0)
	if err != nil {
		t.Fatalf("error creating osbuild manifest: %v", err)
	}

	_, err = workerServer.EnqueueOSBuild(arch.Name(), &worker.OSBuildJob{Manifest: manifest})
	require.NoError(t, err)

	client, err := worker.NewClient(proxySrv.URL, nil, &offlineToken, &oauthSrv.URL)
	require.NoError(t, err)
	job, err := client.RequestJob([]string{"osbuild"}, arch.Name())
	require.NoError(t, err)
	r := strings.NewReader("artifact contents")
	require.NoError(t, job.UploadArtifact("some-artifact", r))
	c, err := job.Canceled()
	require.False(t, c)
}
