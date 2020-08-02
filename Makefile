go-build:
	go build -mod vendor -o exe .

#docker-build:
#	go mod vendor
#	docker build -t soichisumi0/http-sample-app:$(TAG) .
#
#docker-push:
#	make docker-build
#	docker push soichisumi0/http-sample-app:$(TAG)

compose-up:
	go mod vendor
	docker-compose up --force-recreate --build
	rm -rf ./vendor

compose-down:
	docker-compose down

mysql-client:
	mysql -u root -p -h 0.0.0.0 -P 33306