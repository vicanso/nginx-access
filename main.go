package main

import (
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	client "github.com/influxdata/influxdb/client/v2"

	"github.com/ziutek/syslog"
)

type handler struct {
	// To simplify implementation of our handler we embed helper
	// syslog.BaseHandler struct.
	*syslog.BaseHandler
}

type fileLog struct {
	date string
	fd   *os.File
}

var nginxFileLog = fileLog{}

// 将日志写到文件中
func writeToFile(logPath string, buf []byte) {
	now := time.Now()
	date := now.Format("2006-01-02")
	if nginxFileLog.date != date {
		nginxFileLog.date = date
		if nginxFileLog.fd != nil {
			nginxFileLog.fd.Close()
		}
		nginxFileLog.fd = nil
	}

	if nginxFileLog.fd == nil {
		file := logPath + "/" + date
		fd, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0666)
		if err != nil {
			log.Println("Open file fail:", err)
			return
		}
		nginxFileLog.fd = fd
	}
	nginxFileLog.fd.Write(append(buf, '\n'))
}

// get the spdy tag
func getSpdy(time float64) string {
	if time < 100 {
		return "0"
	}
	if time < 300 {
		return "1"
	}
	if time < 1000 {
		return "2"
	}
	if time < 3000 {
		return "3"
	}
	return "4"
}

// 对nginx日志分析，写入性能统计数据
func analyse(str string) *client.Point {
	arr := strings.SplitN(strings.TrimSpace(str), "\"", -1)

	limitLength := 16
	result := make([]string, 0, limitLength*2)
	for index, item := range arr {
		trimItem := strings.TrimSpace(item)
		if len(trimItem) == 0 {
			continue
		}
		// http via
		if index == 7 {
			result = append(result, trimItem)
		} else if index == 3 {
			// content type
			reg := regexp.MustCompile(`/[a-z]+;`)
			contentType := reg.FindString(trimItem)
			if len(contentType) != 0 {
				contentType = contentType[1 : len(contentType)-1]
			} else {
				contentType = "unknown"
			}
			result = append(result, contentType)
		} else {
			tmpArr := strings.SplitN(trimItem, " ", -1)
			result = append(result, tmpArr...)
		}
	}

	if len(result) != limitLength {
		return nil
	}

	status, _ := strconv.ParseInt(result[9], 10, 32)
	responseTime, _ := strconv.ParseFloat(result[11], 64)
	requestTime, _ := strconv.ParseFloat(result[12], 64)
	bytes, _ := strconv.ParseInt(result[10], 10, 32)

	fields := map[string]interface{}{
		"ip":           result[2],
		"track":        result[3],
		"responseId":   result[4],
		"url":          result[7],
		"status":       status,
		"bytes":        bytes,
		"responseTime": responseTime,
		"requestTime":  requestTime,
		"referrer":     result[14],
		"via":          result[15],
	}
	tags := map[string]string{
		"host":        result[5],
		"method":      strings.ToUpper(result[6]),
		"type":        result[9][0:1],
		"spdy":        getSpdy(requestTime),
		"contentType": result[13],
	}
	pt, err := client.NewPoint("nginx_access", tags, fields, time.Now())
	if err != nil {
		log.Println(err)
	}
	return pt
}

// Simple fiter for named/bind messages which can be used with BaseHandler
func filter(m *syslog.Message) bool {
	// only for nginx
	return true
	// return m.Tag == "nginx"
}

func newHandler() *handler {
	h := handler{syslog.NewBaseHandler(5, filter, false)}
	go h.mainLoop() // BaseHandler needs some gorutine that reads from its queue
	return &h
}

// mainLoop reads from BaseHandler queue using h.Get and logs messages to stdout
func (h *handler) mainLoop() {
	logPath := os.Getenv("LOG_PATH")
	if len(logPath) == 0 {
		logPath = "/logs"
	}
	database := "telegraf"
	batchSize := 50
	influxServer := os.Getenv("INFLUX")
	if len(influxServer) == 0 {
		influxServer = "http://127.0.0.1:8086"
	}
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr:     influxServer,
		Username: os.Getenv("USER"),
		Password: os.Getenv("PASS"),
	})
	if err != nil {
		log.Panic(err)
	}
	_, _, err = c.Ping(time.Second * 5)
	if err != nil {
		log.Panic(err)
	}
	// Create a new point batch
	bp, err := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})
	if err != nil {
		log.Panic(err)
	}
	count := 0
	for {
		m := h.Get()
		if m == nil {
			break
		}
		writeToFile(logPath, []byte(m.Content1))
		pt := analyse(m.Content1)
		if pt == nil {
			continue
		}
		if bp == nil {
			bp, err = client.NewBatchPoints(client.BatchPointsConfig{
				Database: database,
			})
			// create point batch fail, contine
			if err != nil {
				log.Println(err)
				continue
			}
		}
		count++
		bp.AddPoint(pt)
		log.Println(count)
		// batch write to influxdb
		if count >= batchSize {
			if err := c.Write(bp); err != nil {
				log.Println(err)
			}
			count = 0
			bp = nil
		}
	}
	log.Println("Exit handler")
	h.End()
}

func main() {
	// Create a server with one handler and run one listen gorutine
	s := syslog.NewServer()
	s.AddHandler(newHandler())
	s.Listen("0.0.0.0:3412")
	log.Println("server is linsten on 3412")

	// Wait for terminating signal
	sc := make(chan os.Signal, 2)
	signal.Notify(sc, syscall.SIGTERM, syscall.SIGINT)
	<-sc

	// Shutdown the server
	log.Println("Shutdown the server...")
	s.Shutdown()
	log.Println("Server is down")
}
