GOOS=linux go build

docker build -t vicanso/nginx-access .

rm ./nginx-access
