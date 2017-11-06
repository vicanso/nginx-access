# nginx-access

Get the log by syslog, save to file and get the stats

## docker build

```
docker build -t vicanso/nginx-access .
```

## docker env

- `LOG_PATH` the path to save nginx's access log, default is '/logs'

- `INFLUX` the uri for influxdb server, default is 'http://127.0.0.1:8086'

- `USER` the user for influxdb

- `PASS` the password for influxdb



## docker run

```
docker run -d --restart=always \
  -v /data/nginx:/logs \
  -e INFLUX=http://172.0.0.1:8086 \
  -e USER=user \
  -e PASS=password \
  -p 3412:3412 \
  -p 3412:3412/udp \
  vicanso/nginx-access
``` 
