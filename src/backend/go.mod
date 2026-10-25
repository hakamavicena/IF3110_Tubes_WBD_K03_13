module backend

go 1.27.3

replace github.com/hakamavicena/if3110-dbengine => ./dbengine

require (
	github.com/google/uuid v1.6.0
	github.com/hakamavicena/if3110-dbengine v0.0.0-00010101000000-000000000000
	github.com/jackc/pgx/v5 v5.7.2
	github.com/joho/godotenv v1.5.1
	github.com/lib/pq v1.12.3
	golang.org/x/crypto v0.31.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.10.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)
