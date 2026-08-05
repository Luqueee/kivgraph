module example.com/luque-fixture/consumer-b

go 1.24

require example.com/luque-fixture/legacy v1.0.0

replace example.com/luque-fixture/legacy => ./internal/legacy
