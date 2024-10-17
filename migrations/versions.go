package migrations

import "github.com/formancehq/go-libs/v2/time"

type VersionTable struct {
	ID        int       `bun:"id,type:serial,pk,scanonly"`
	VersionID int       `bun:"version_id,type:bigint,notnull"`
	Timestamp time.Time `bun:"tstamp,type:timestamp,default:now()"`
	IsApplied bool      `bun:"is_applied,type:boolean,notnull"`
}
