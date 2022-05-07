package optimizer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/client9/csstool"
	"github.com/go-mojito/mojito"
	"github.com/mattn/go-zglob"
	"github.com/tdewolff/parse/buffer"
)

var (
	cacheKeyPrefix = "optimizer_css"
)

func CSS(ctx mojito.Context, cache mojito.Cache, logger mojito.Logger, next func() error) error {
	path := ctx.Request().GetRequest().URL.Path
	cacheKey := cssCacheKey(path)
	if !ctx.Request().GetRequest().URL.Query().Has("critical") {
		return next()
	}

	// Determine if a cached version of the optimized CSS is available
	if exists, err := cache.Contains(cacheKey); err == nil && exists {
		// Great we have an optimized version available, skip handlers down the line
		var optimizedCss []byte
		err := cache.Get(cacheKey, &optimizedCss)
		if err == nil {
			ctx.Response().Header().Set("content-type", "text/css")
			ctx.Response().Write(optimizedCss)
			return nil
		}
		logger.Errorf("Optimizer Cache failed despite existing cache, dropping cache entry. Error: %v", err)
		_ = cache.Delete(cacheKey)
	}

	// Setup the fake response so we can intercept the css delivered by the asset handler down the line
	originalWriter := ctx.Response().GetWriter()
	fakeWriter := NewFakeResponse()
	fakeWriter.Headers = originalWriter.Header()
	ctx.Response().SetWriter(fakeWriter)

	// If the asset handler fails, the optimizer aborts and returns whatever was produced by the handler
	if err := next(); err != nil {
		originalWriter.WriteHeader(fakeWriter.Status)
		originalWriter.Write(fakeWriter.Body)
		return err
	}

	// No optimized CSS is cached or the cache failed
	fsPath := mojito.ResourcesDir()
	c := csstool.NewCSSCount()
	files, err := zglob.Glob(fsPath + "/templates/**/*.mojito")
	if err != nil {
		log.Fatalf("FAIL: %s", err)
	}
	for _, f := range files {
		r, err := os.Open(f)
		if err != nil {
			log.Fatalf("FAIL: %s", err)
		}
		err = c.Add(r)
		if err != nil {
			log.Fatalf("FAIL: %s", err)
		}
		r.Close()
	}

	// now get CSS file
	m := csstool.NewTagMatcher(c.List())
	cf := csstool.NewCSSFormat(0, false, m)
	cf.RemoveSourceMap = true
	var buf []byte
	writeBuf := buffer.NewWriter(buf)
	err = cf.Format(buffer.NewReader(fakeWriter.Body), writeBuf)
	if err != nil {
		return err
	}
	cache.Set(cacheKey, writeBuf.Bytes())
	originalWriter.Header().Set("content-type", "text/css")
	originalWriter.Write(writeBuf.Bytes())
	return nil
}

func cssCacheKey(path string) string {
	return fmt.Sprintf("%s_%s", cacheKeyPrefix, hashString(path))
}

// KeyHash returns the SHA256 hash of a string for caching purposes
func hashString(key string) string {
	h := sha256.New()
	h.Write([]byte(key))
	return hex.EncodeToString(h.Sum(nil))
}
