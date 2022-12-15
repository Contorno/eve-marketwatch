package marketwatch

import (
	"context"
	"log"
	"time"
)

func (s *MarketWatch) startUpMarketWorkers() error {
	// Get all the regions and fire up workers for each
	regions, res, err := s.esi.ESI.UniverseApi.GetUniverseRegions(context.Background(), nil)
	defer func() {
		err := res.Body.Close()
		if err != nil {
			log.Println(err)
		}
	}()

	if err != nil {
		return err
	}

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
