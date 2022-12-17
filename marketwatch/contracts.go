package marketwatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/contorno/goesi/esi"
	"github.com/contorno/optional"
	"github.com/getsentry/sentry-go"
	"github.com/prometheus/client_golang/prometheus"
)

func (s *MarketWatch) contractWorker(regionID int32, localHub *sentry.Hub) {
	localHub.ConfigureScope(
		func(scope *sentry.Scope) {
			scope.SetTag("locationHash", "go#contract-worker")
		},
	)

	// For totalization
	wg := sync.WaitGroup{}

	// Loop forever
	for {
		start := time.Now()
		numContracts := 0

		// Return Channels
		rchan := make(chan []esi.GetContractsPublicRegionId200Ok, 100000)
		echan := make(chan error, 100000)

		contracts, res, err := s.esi.ESI.ContractsApi.GetContractsPublicRegionId(
			context.Background(), regionID, nil,
		)
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
			continue
		}
		rchan <- contracts

		// Figure out if there are more pages
		pages, _ := getPages(res)
		duration := timeUntilCacheExpires(res)
		if duration.Minutes() < 3 {
			fmt.Printf("%d contract too close to window: waiting %s\n", regionID, duration.String())
			time.Sleep(duration)
			continue
		}

		// Get the other pages concurrently
		for pages > 1 {
			wg.Add(1) // count what's running
			go func(page int32, localHub *sentry.Hub) {
				localHub.ConfigureScope(
					func(scope *sentry.Scope) {
						scope.SetTag("locationHash", "go#contract-worker-get-contracts-public-region-id-page")
					},
				)

				defer wg.Done() // release when done

				// Throttle down the requests to avoid bans.
				sleepRandom(3, 0.5)

				contracts, r, err := s.esi.ESI.ContractsApi.GetContractsPublicRegionId(
					context.Background(), regionID, &esi.GetContractsPublicRegionIdOpts{Page: optional.NewInt32(page)},
				)
				if err != nil {
					echan <- err
					return
				}

				// Are we too close to the end of the window?
				duration = timeUntilCacheExpires(r)
				if duration.Seconds() < 20 {
					echan <- errors.New("contract too close to end of window")
					return
				}

				// Add the contracts to the channel
				rchan <- contracts
			}(pages, sentry.CurrentHub().Clone())
			pages--
		}

		wg.Wait() // Wait for everything to finish

		// Close the channels
		close(rchan)
		close(echan)

		for err := range echan {
			sentry.CaptureException(err)
			log.Println(err)
			// Start over if any requests failed
			continue
		}

		var changes []ContractChange
		var newContracts []FullContract
		// Add all the contracts together
		for o := range rchan {
		Restart:
			for i := range o {
				// Ignore expired contracts
				if o[i].DateExpired.Before(time.Now()) {
					continue
				}

				contract := Contract{Touched: start, Contract: FullContract{Contract: o[i]}}

				if o[i].Type_ == "item_exchange" || o[i].Type_ == "auction" {
					err := s.getContractItems(&contract)
					if err != nil {
						sentry.CaptureException(err)
						goto Restart
					}
				}

				if o[i].Type_ == "auction" {
					err := s.getContractBids(&contract)
					if err != nil {
						sentry.CaptureException(err)
						goto Restart
					}
				}

				change, isNew := s.storeContract(int64(regionID), contract)
				numContracts++
				if change.Changed && !isNew {
					changes = append(changes, change)
				}
				if isNew {
					newContracts = append(newContracts, contract.Contract)
				}
			}
		}
		deletions := s.expireContracts(int64(regionID), start)

		// Log metrics
		metricContractTimePull.With(
			prometheus.Labels{
				"locationID": strconv.FormatInt(int64(regionID), 10),
			},
		).Observe(float64(time.Since(start).Nanoseconds()) / float64(time.Millisecond))

		if len(newContracts) > 0 {
			s.broadcast.Broadcast(
				"contract", Message{
					Action:  "contractAddition",
					Payload: newContracts,
				},
			)
		}

		// Only bids really change.
		if len(changes) > 0 {
			s.broadcast.Broadcast(
				"contract", Message{
					Action:  "contractChange",
					Payload: changes,
				},
			)
		}

		if len(deletions) > 0 {
			s.broadcast.Broadcast(
				"contract", Message{
					Action:  "contractDeletion",
					Payload: deletions,
				},
			)
		}

		// Sleep until the cache timer expires, plus a little.
		time.Sleep(duration)
	}
}

// getContractItems for a single contract. Must be prefilled with the contract.
func (s *MarketWatch) getContractItems(contract *Contract) error {
	wg := sync.WaitGroup{}

	rchan := make(chan []esi.GetContractsPublicItemsContractId200Ok, 100000)
	echan := make(chan error, 100000)

	// Throttle down the requests to avoid bans.
	sleepRandom(5, 0.5)

	items, res, err := s.esi.ESI.ContractsApi.GetContractsPublicItemsContractId(
		context.Background(), contract.Contract.Contract.ContractId, nil,
	)

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			sentry.CaptureException(err)
			log.Println(err)
		}
	}(res.Body)

	if err != nil {
		return err
	}

	rchan <- items
	pages, _ := getPages(res)

	for pages > 1 {
		wg.Add(1) // count what's running
		go func(page int32, localHub *sentry.Hub) {
			localHub.ConfigureScope(
				func(scope *sentry.Scope) {
					scope.SetTag("locationHash", "go#get-contract-items")
				},
			)
			defer wg.Done()

			// Throttle down the requests to avoid bans.
			sleepRandom(5, 0.5)

			items, itemsRes, err := s.esi.ESI.ContractsApi.GetContractsPublicItemsContractId(
				context.Background(),
				contract.Contract.Contract.ContractId,
				&esi.GetContractsPublicItemsContractIdOpts{Page: optional.NewInt32(page)},
			)
			defer func(Body io.ReadCloser) {
				err := Body.Close()
				if err != nil {
					sentry.CaptureException(err)
				}
			}(itemsRes.Body)

			if err != nil {
				echan <- err
			} else {
				rchan <- items
			}
		}(pages, sentry.CurrentHub().Clone())
		pages--
	}

	wg.Wait()

	// Close the channels
	close(rchan)
	close(echan)

	for err := range echan {
		return err
	}

	// Add all the contracts together
	for o := range rchan {
		contract.Contract.Items = append(contract.Contract.Items, o...)
	}

	return nil
}

// getContractBids for a single contract. Must be prefilled with the contract.
func (s *MarketWatch) getContractBids(contract *Contract) error {
	wg := sync.WaitGroup{}

	// Return Channels
	rchan := make(chan []esi.GetContractsPublicBidsContractId200Ok, 100000)
	echan := make(chan error, 100000)

	// Throttle down the requests to avoid bans.
	sleepRandom(3, 0.5)

	bids, res, err := s.esi.ESI.ContractsApi.GetContractsPublicBidsContractId(
		context.Background(), contract.Contract.Contract.ContractId, nil,
	)
	if err != nil {
		sentry.CaptureException(err)
		log.Println(err)
	}
	rchan <- bids
	pages, _ := getPages(res)

	// Get the other pages concurrently
	for pages > 1 {
		wg.Add(1) // count what's running
		go func(page int32, localHub *sentry.Hub) {
			localHub.ConfigureScope(
				func(scope *sentry.Scope) {
					scope.SetTag("locationHash", "go#get-contract-bids")
				},
			)

			defer wg.Done()
			// Throttle down the requests to avoid bans.
			sleepRandom(5, 0.5)

			bids, bidsRes, err := s.esi.ESI.ContractsApi.GetContractsPublicBidsContractId(
				context.Background(),
				contract.Contract.Contract.ContractId,
				&esi.GetContractsPublicBidsContractIdOpts{Page: optional.NewInt32(page)},
			)
			defer func(Body io.ReadCloser) {
				err := Body.Close()
				if err != nil {
					sentry.CaptureException(err)
					log.Println(err)
				}
			}(bidsRes.Body)

			if err != nil {
				echan <- err
				return
			}

			rchan <- bids
		}(pages, sentry.CurrentHub().Clone())
		pages--
	}

	wg.Wait()

	close(rchan)
	close(echan)

	for err := range echan {
		return err
	}

	// Add all the bids together
	for o := range rchan {
		contract.Contract.Bids = append(contract.Contract.Bids, o...)
	}

	return nil
}

// Metrics
var (
	metricContractTimePull = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "evemarketwatch",
			Subsystem: "contract",
			Name:      "pull",
			Help:      "Market Pull Statistics",
			Buckets:   prometheus.ExponentialBuckets(10, 1.6, 20),
		}, []string{"locationID"},
	)
)

func init() {
	prometheus.MustRegister(
		metricContractTimePull,
	)
}
