package telemetry

import "github.com/jackc/pgx/v5/pgxpool"

func PGXPoolStat(p *pgxpool.Pool) PoolStat { return AggregatePoolStat([]*pgxpool.Pool{p}) }

func AggregatePoolStat(pools []*pgxpool.Pool) PoolStat {
	var s PoolStat
	for _, p := range pools {
		if p == nil {
			continue
		}
		st := p.Stat()
		s.Acquired += st.AcquiredConns()
		s.Idle += st.IdleConns()
		s.Max += st.MaxConns()
		s.AcquireCount += st.AcquireCount()
		s.WaitCount += st.EmptyAcquireCount()
		s.WaitDuration += st.EmptyAcquireWaitTime()
	}
	return s
}
