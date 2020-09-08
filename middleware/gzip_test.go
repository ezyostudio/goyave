package middleware

import (
	"compress/gzip"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/System-Glitch/goyave/v3"
)

type GzipMiddlewareTestSuite struct {
	goyave.TestSuite
}

type closeableChildWriter struct {
	io.Writer
	closed bool
}

func (w *closeableChildWriter) Close() error {
	w.closed = true
	return nil
}

func (suite *GzipMiddlewareTestSuite) TestGzipMiddleware() {
	handler := func(response *goyave.Response, r *goyave.Request) {
		response.String(http.StatusOK, "hello world")
	}
	rawRequest := httptest.NewRequest("GET", "/", nil)
	request := suite.CreateTestRequest(rawRequest)
	result := suite.Middleware(Gzip(), request, handler)
	body, err := ioutil.ReadAll(result.Body)
	if err != nil {
		panic(err)
	}
	suite.Equal("hello world", string(body)) // Not compressed

	rawRequest = httptest.NewRequest("GET", "/", nil)
	rawRequest.Header.Set("Accept-Encoding", "gzip")
	request = suite.CreateTestRequest(rawRequest)
	result = suite.Middleware(Gzip(), request, handler)
	suite.Equal("gzip", result.Header.Get("Content-Encoding"))

	reader, err := gzip.NewReader(result.Body)
	if err != nil {
		panic(err)
	}
	body, err = ioutil.ReadAll(reader)
	if err != nil {
		panic(err)
	}
	suite.Equal("hello world", string(body))
}

func (suite *GzipMiddlewareTestSuite) TestCloseNonCloseable() {
	rawRequest := httptest.NewRequest("GET", "/", nil)
	rawRequest.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	writer, _ := gzip.NewWriterLevel(recorder, gzip.BestCompression)
	compressWriter := &gzipWriter{
		Writer:         writer,
		ResponseWriter: recorder,
	}
	if _, err := compressWriter.Write([]byte("hello world")); err != nil {
		panic(err)
	}
	compressWriter.Close()

	result := recorder.Result()
	reader, err := gzip.NewReader(result.Body)
	if err != nil {
		panic(err)
	}

	body, err := ioutil.ReadAll(reader)
	if err != nil {
		panic(err)
	}

	suite.Equal("hello world", string(body))
}

func (suite *GzipMiddlewareTestSuite) TestCloseChild() {
	closeableWriter := &closeableChildWriter{closed: false}
	suite.RunServer(func(router *goyave.Router) {
		router.Middleware(func(next goyave.Handler) goyave.Handler {
			return func(response *goyave.Response, r *goyave.Request) {
				closeableWriter.Writer = response.Writer()
				response.SetWriter(closeableWriter)
				next(response, r)
			}
		})
		router.Middleware(Gzip())
		router.Route("GET", "/test", func(response *goyave.Response, r *goyave.Request) {
			response.String(http.StatusOK, "hello world")
		})
	}, func() {
		resp, err := suite.Get("/test", nil)
		if err != nil {
			suite.Fail(err.Error())
		}
		resp.Body.Close()
		suite.True(closeableWriter.closed)
	})
}

func (suite *GzipMiddlewareTestSuite) TestGzipMiddlewareInvalidLevel() {
	suite.Panics(func() { GzipLevel(-3) })
	suite.Panics(func() { GzipLevel(10) })
}

func (suite *GzipMiddlewareTestSuite) TestUpgrade() {
	suite.RunServer(func(router *goyave.Router) {
		router.Middleware(Gzip())
		router.Route("GET", "/test", func(response *goyave.Response, r *goyave.Request) {
			response.String(http.StatusOK, "hello world")
		})
	}, func() {
		headers := map[string]string{
			"Accept-Encoding": "gzip",
			"Upgrade":         "example/1, foo/2",
		}
		resp, err := suite.Get("/test", headers)
		if err != nil {
			suite.Fail(err.Error())
		}
		defer resp.Body.Close()
		body := suite.GetBody(resp)
		suite.Equal("hello world", string(body))
		suite.NotEqual("gzip", resp.Header.Get("Content-Encoding"))
	})
}

func (suite *GzipMiddlewareTestSuite) TestWriteFile() {
	suite.RunServer(func(router *goyave.Router) {
		router.Middleware(Gzip())
		router.Route("GET", "/test", func(response *goyave.Response, r *goyave.Request) {
			response.File("resources/custom_config.json")
		})
	}, func() {
		resp, err := suite.Get("/test", map[string]string{"Accept-Encoding": "gzip"})
		if err != nil {
			suite.Fail(err.Error())
		}
		defer resp.Body.Close()

		// reader, err := gzip.NewReader(resp.Body)
		// if err != nil {
		// 	panic(err)
		// }

		// body, err := ioutil.ReadAll(reader)
		// if err != nil {
		// 	panic(err)
		// }
		body, _ := ioutil.ReadAll(resp.Body)
		suite.Equal("application/json", resp.Header.Get("Content-Type"))
		suite.Equal("{\n    \"custom-entry\": \"value\"\n}", string(body))
	})
}

func TestGzipMiddlewareTestSuite(t *testing.T) {
	goyave.RunTest(t, new(GzipMiddlewareTestSuite))
}
