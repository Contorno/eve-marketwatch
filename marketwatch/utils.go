package marketwatch

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"strconv"
	"time"

	"github.com/contorno/goesi"
)

func getPages(r *http.Response) (int32, error) {
	// Decode the page into int32. Return if this fails as there were no extra pages.
	pagesInt, err := strconv.Atoi(r.Header.Get("x-pages"))
	if err != nil {
		return 0, err
	}
	pages := int32(pagesInt)
	return pages, err
}

func timeUntilCacheExpires(r *http.Response) time.Duration {
	duration := time.Until(goesi.CacheExpires(r))
	if duration < time.Second {
		duration = time.Second * 10
	} else {
		duration += time.Second * 15
	}
	return duration
}

// Sleep for a random amount of time to avoid hitting the rate limit
func sleepRandom(max int64, additional float64) {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(max*10))
	time.Sleep(time.Duration(additional+float64(nBig.Int64())/10) * time.Second)
}
