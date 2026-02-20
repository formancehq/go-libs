package migrations

import "github.com/formancehq/go-libs/v4/time"

type Version struct {
	ID            int       `bun:"id,type:serial,pk,scanonly"`
	VersionID     int       `bun:"version_id,type:bigint,notnull"`
	Timestamp     time.Time `bun:"tstamp,type:timestamp,default:now()"`
	IsApplied     bool      `bun:"is_applied,type:boolean,notnull"`
	MaxCounter    int       `bun:"max_counter,type:numeric,nullzero"`
	ActualCounter int       `bun:"actual_counter,type:numeric,nullzero"`
	TerminatedAt  time.Time `bun:"terminated_at,type:timestamp,nullzero"`
}
