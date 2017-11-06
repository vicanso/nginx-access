FROM alpine

ADD ./nginx-access /

CMD ["/nginx-access"]