package marketwatch

import (
	"context"
	"log"
	"net/http"
	"time"
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
			return err
		} else {
			time.Sleep(time.Second * 5)
		}
	}

	defer func() {
		err := res.Body.Close()
		if err != nil {
			log.Println(err)
		}
	}()

	for _, region := range regions {
		// Prebuild the maps
		s.createMarketStore(int64(region))
		s.createContractStore(int64(region))
		// Ignore non-market regions
		if region < 11000000 || region == 11000031 {
			time.Sleep(time.Millisecond * 500)
			go s.marketWorker(region)
			go s.contractWorker(region)
		}
	}

	return nil
}
