module example.com/kivgraph-fixture/consumer-b

go 1.24

require example.com/kivgraph-fixture/legacy v1.0.0

replace example.com/kivgraph-fixture/legacy => ./internal/legacy
