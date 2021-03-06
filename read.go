package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/antihax/goesi/esi"
	"github.com/cockroachdb/cockroach-go/crdb"
	"github.com/pkg/errors"
)

func (s *EFContext) CreateTables() {
	if _, err := s.DB.Exec(`
		DROP TABLE IF EXISTS hashes;

		DROP TABLE IF EXISTS fits;

		DROP TABLE IF EXISTS killmails;

		CREATE TABLE hashes (
			id        INT4 PRIMARY KEY,
			hash      STRING NOT NULL,
			processed INT4 DEFAULT 0 NOT NULL,
			INDEX (processed)
		);

		CREATE TABLE killmails (
			id        INT4 PRIMARY KEY,
			km        JSONB NOT NULL,
			zkb JSONB NOT NULL,
			processed INT4 DEFAULT 0 NOT NULL,
			INDEX (processed)
		);

		CREATE TABLE fits (
			killmail    INT4,
			ship        INT4 NOT NULL,
			cost        INT8,
			solarsystem INT4 NOT NULL,
			hi          JSONB NOT NULL,
			med         JSONB NOT NULL,
			low         JSONB NOT NULL,
			rig         JSONB NOT NULL,
			sub         JSONB NOT NULL,
			items       JSONB NOT NULL,
			PRIMARY KEY (killmail DESC),
			INVERTED INDEX (items)
		);
	`); err != nil {
		log.Fatal(err)
	}
}

const (
	ProcHashFetched = 1

	ProcKMFitAdded  = 1
	ProcKMZkbAdded  = 2
	ProcKMCostAdded = 3
)

type KM esi.GetKillmailsKillmailIdKillmailHashOk

type Slot int32

const (
	LoSlot0 Slot = 11 + iota
	LoSlot1
	LoSlot2
	LoSlot3
	LoSlot4
	LoSlot5
	LoSlot6
	LoSlot7
	MedSlot0
	MedSlot1
	MedSlot2
	MedSlot3
	MedSlot4
	MedSlot5
	MedSlot6
	MedSlot7
	HiSlot0
	HiSlot1
	HiSlot2
	HiSlot3
	HiSlot4
	HiSlot5
	HiSlot6
	HiSlot7
)

const (
	RigSlot0 Slot = 92 + iota
	RigSlot1
	RigSlot2
	RigSlot3
	RigSlot4
	RigSlot5
	RigSlot6
	RigSlot7
)

const (
	SubSlot0 Slot = 125 + iota
	SubSlot1
	SubSlot2
	SubSlot3
	SubSlot4
	SubSlot5
	SubSlot6
	SubSlot7
)

func IsHigh(s Slot) bool   { return s.IsHigh() }
func IsMedium(s Slot) bool { return s.IsMedium() }
func IsLow(s Slot) bool    { return s.IsLow() }
func IsRig(s Slot) bool    { return s.IsRig() }
func IsSub(s Slot) bool    { return s.IsSub() }

func (s Slot) IsHigh() bool {
	return s >= HiSlot0 && s <= HiSlot7
}

func (s Slot) IsMedium() bool {
	return s >= MedSlot0 && s <= MedSlot7
}

func (s Slot) IsLow() bool {
	return s >= LoSlot0 && s <= LoSlot7
}

func (s Slot) IsRig() bool {
	return s >= RigSlot0 && s <= RigSlot7
}

func (s Slot) IsSub() bool {
	return s >= SubSlot0 && s <= SubSlot7
}

// FetchHashes listens on the zkillboard redisq API and populates the hashes
// and killmails tables with results. As soon as zkillboard has no more results
// or ctx is cancelled this function returns.
func (s *EFContext) FetchHashes(ctx context.Context) {
	// We don't want the db txn to fail if ctx is canceled.
	dbCtx := context.Background()
	for {
		if ctx.Err() != nil {
			return
		}

		// Use a low ttw so the request stops as soon as possible to
		// lower the google cloud run request times.
		resp, _ := http.Get("https://redisq.zkillboard.com/listen.php?queueID=fittin.gs&ttw=1")
		if resp == nil || resp.StatusCode != 200 {
			return
		}
		var pkg ZKillPackage
		_ = json.NewDecoder(resp.Body).Decode(&pkg)
		resp.Body.Close()
		if pkg.Package == nil {
			return
		}
		rawKM, err := json.Marshal(pkg.Package.Killmail)
		if err != nil {
			panic(err)
		}
		rawZKB, err := json.Marshal(pkg.Package.Zkb)
		if err != nil {
			panic(err)
		}
		if err := crdb.ExecuteTx(dbCtx, s.DB, nil, func(txn *sql.Tx) error {
			if _, err := txn.ExecContext(dbCtx, `
				INSERT
				INTO
					hashes (id, hash, processed)
				VALUES
					($1, $2, $3)
				ON CONFLICT
					(id)
				DO
					NOTHING
			`, pkg.Package.KillID, pkg.Package.Zkb.Hash, ProcHashFetched); err != nil {
				return err
			}
			if _, err := txn.ExecContext(dbCtx, `
				INSERT
				INTO
					killmails (id, km, zkb)
				VALUES
					($1, $2, $3)
				ON CONFLICT
					(id)
				DO
					NOTHING
			`, pkg.Package.KillID, rawKM, rawZKB); err != nil {
				return err
			}
			return nil
		}); err != nil {
			log.Print(err)
		} else {
			log.Println("inserted", pkg.Package.KillID)
		}
	}
}

type ZKillPackage struct {
	Package *struct {
		KillID   int `json:"killID"`
		Killmail struct {
			Attackers []struct {
				CorporationID  int  `json:"corporation_id"`
				DamageDone     int  `json:"damage_done"`
				FinalBlow      bool `json:"final_blow"`
				SecurityStatus int  `json:"security_status"`
				ShipTypeID     int  `json:"ship_type_id"`
			} `json:"attackers"`
			KillmailID    int       `json:"killmail_id"`
			KillmailTime  time.Time `json:"killmail_time"`
			SolarSystemID int       `json:"solar_system_id"`
			Victim        struct {
				AllianceID    int `json:"alliance_id"`
				CharacterID   int `json:"character_id"`
				CorporationID int `json:"corporation_id"`
				DamageTaken   int `json:"damage_taken"`
				Items         []struct {
					Flag              int `json:"flag"`
					ItemTypeID        int `json:"item_type_id"`
					QuantityDropped   int `json:"quantity_dropped,omitempty"`
					Singleton         int `json:"singleton"`
					QuantityDestroyed int `json:"quantity_destroyed,omitempty"`
				} `json:"items"`
				Position struct {
					X float64 `json:"x"`
					Y float64 `json:"y"`
					Z float64 `json:"z"`
				} `json:"position"`
				ShipTypeID int `json:"ship_type_id"`
			} `json:"victim"`
		} `json:"killmail"`
		Zkb Zkb `json:"zkb"`
	} `json:"package"`
}

type Zkb struct {
	LocationID  int     `json:"locationID"`
	Hash        string  `json:"hash"`
	FittedValue float64 `json:"fittedValue"`
	TotalValue  float64 `json:"totalValue"`
	Points      int     `json:"points"`
	Npc         bool    `json:"npc"`
	Solo        bool    `json:"solo"`
	Awox        bool    `json:"awox"`
	Href        string  `json:"href"`
}

func (s *EFContext) ProcessFits(ctx context.Context) {
	dbCtx := context.Background()
	for {
		if ctx.Err() != nil {
			return
		}

		if err := crdb.ExecuteTx(dbCtx, s.DB, nil, s.processKM); err != nil {
			log.Printf("process fits: %+v", err)
			return
		}
	}
}

func (s *EFContext) processKM(tx *sql.Tx) error {
	var rawKM, rawZKB []byte
	if err := tx.QueryRow(`SELECT km, zkb FROM killmails WHERE processed = 0 LIMIT 1`).Scan(&rawKM, &rawZKB); err != nil {
		return err
	}

	var km KM
	if err := json.Unmarshal(rawKM, &km); err != nil {
		panic(err)
	}
	var zkb Zkb
	if len(rawZKB) > 0 {
		if err := json.Unmarshal(rawZKB, &zkb); err != nil {
			panic(err)
		}
	}
	// Only process fits where there's something fitted to a high
	// slot. This filters out boring fits and stuff like drones.
	hi, _, _, _, _, items := km.Items(s)
	hiCount := 0
	for _, h := range hi {
		if h.ID > 0 {
			hiCount++
		}
	}
	if hiCount > 0 {
		v := km.Victim
		var args []interface{}
		args = append(args, km.KillmailId, v.ShipTypeId, km.SolarSystemId)
		// Find items per slot.
		filter := func(f func(Slot) bool) []byte {
			var items []int32
			for _, i := range v.Items {
				if f(Slot(i.Flag)) {
					items = append(items, i.ItemTypeId)
				}
			}
			enc, err := json.Marshal(&items)
			if err != nil {
				panic(err)
			}
			return enc
		}
		args = append(args, filter(IsHigh))
		args = append(args, filter(IsMedium))
		args = append(args, filter(IsLow))
		args = append(args, filter(IsRig))
		args = append(args, filter(IsSub))
		enc, err := json.Marshal(&items)
		if err != nil {
			panic(err)
		}
		args = append(args, enc)
		args = append(args, int64(zkb.FittedValue))

		if _, err := tx.Exec(`
			INSERT
			INTO
				fits
					(
						killmail,
						ship,
						solarsystem,
						hi,
						med,
						low,
						rig,
						sub,
						items,
						cost
					)
			VALUES
				($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT
				(killmail)
			DO
				NOTHING
		`, args...); err != nil {
			return errors.Wrap(err, "upsert")
		}
	}
	proc := ProcKMFitAdded
	if zkb.FittedValue > 0 {
		proc = ProcKMCostAdded
	}
	if _, err := tx.Exec(`UPDATE killmails SET processed = $2 WHERE id = $1`, km.KillmailId, proc); err != nil {
		return errors.Wrap(err, "update killmails")
	}

	fmt.Println("processed km", km.KillmailId, "to", proc)
	return nil
}

func (k KM) Items(s *EFContext) (hi, med, low, rig, sub [8]ItemCharge, items []int32) {
	items = append(items, k.Victim.ShipTypeId)
	for _, i := range k.Victim.Items {
		flag := Slot(i.Flag)
		item := s.Global.Items[i.ItemTypeId]
		charge := s.Global.Groups[item.Group].IsCharge()
		var n Slot
		var cur *[8]ItemCharge
		switch {
		case IsHigh(flag):
			n = HiSlot0
			cur = &hi
		case IsMedium(flag):
			n = MedSlot0
			cur = &med
		case IsLow(flag):
			n = LoSlot0
			cur = &low
		case IsRig(flag):
			n = RigSlot0
			cur = &rig
		case IsSub(flag):
			n = SubSlot0
			cur = &sub
		default:
			continue
		}
		n = flag - n
		if charge {
			cur[n].Charge = &item
		} else {
			cur[n].Item = item
		}
		items = append(items, item.ID)
	}
	return
}

type ItemCharge struct {
	Item
	Charge *Item `json:",omitempty"`
}
