package marketwatch

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
)

func (s *MarketWatch) startUpMarketWorkers() error {
	var regions []int32
	var res *http.Response
	var err error
	tries := 0

	for {
		tries++
		regions, res, err = s.esi.ESI.UniverseApi.GetUniverseRegions(context.Background(), nil)

		if err == nil {
			break
		} else if tries > 5 {
			sentry.CaptureException(err)
			return err
		} else {
			time.Sleep(time.Second * 5)
		}
	}

	for _, region := range regions {
		s.createMarketStore(int64(region))
		s.createContractStore(int64(region))
		// Ignore non-market regions
		if region < 11000000 || region == 11000031 {
			time.Sleep(time.Second * 1)
			go s.marketWorker(region, sentry.CurrentHub().Clone())
			go s.contractWorker(region, sentry.CurrentHub().Clone())
		}
	}

	defer func() {
		err := res.Body.Close()
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
		}
	}()

	return nil
}
