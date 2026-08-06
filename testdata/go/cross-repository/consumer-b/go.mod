module example.com/ladygraph-fixture/consumer-b

go 1.24

require example.com/ladygraph-fixture/legacy v1.0.0

replace example.com/ladygraph-fixture/legacy => ./internal/legacy
