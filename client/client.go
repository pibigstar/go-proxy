package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"time"
)

var (
	host       string
	localPort  int
	remotePort int
)

func init() {
	flag.StringVar(&host, "h", "127.0.0.1", "remote server ip")
	flag.IntVar(&localPort, "l", 8080, "the local port")
	flag.IntVar(&remotePort, "r", 3333, "remote server port")
}

type server struct {
	conn net.Conn
	// 数据传输通道
	read  chan []byte
	write chan []byte
	// 异常退出通道
	exit chan error
	// 重连通道
	reConn chan bool
}

// 从Server端读取数据
func (s *server) Read() {
	// 如果10秒钟内没有消息传输，则Read函数会返回一个timeout的错误
	_ = s.conn.SetReadDeadline(time.Now().Add(time.Second * 10))
	for {
		data := make([]byte, 10240)
		n, err := s.conn.Read(data)
		if err != nil && err != io.EOF {
			// 读取超时，发送一个心跳包过去
			if strings.Contains(err.Error(), "timeout") {
				// 3秒发一次心跳
				_ = s.conn.SetReadDeadline(time.Now().Add(time.Second * 3))
				s.conn.Write([]byte("pi"))
				continue
			}
			fmt.Println("从server读取数据失败, ", err.Error())
			s.exit <- err
			runtime.Goexit()
		}

		// 如果收到心跳包, 则跳过
		if data[0] == 'p' && data[1] == 'i' {
			fmt.Println("client收到心跳包")
			continue
		}
		s.read <- data[:n]
	}
}

// 将数据写入到Server端
func (s *server) Write() {
	for {
		select {
		case data := <-s.write:
			_, err := s.conn.Write(data)
			if err != nil && err != io.EOF {
				s.exit <- err
				return
			}
		}
	}
}

type local struct {
	conn net.Conn
	// 数据传输通道
	read  chan []byte
	write chan []byte
	// 有异常退出通道
	exit chan error
}

func (l *local) Read() {

	for {
		data := make([]byte, 10240)
		n, err := l.conn.Read(data)
		if err != nil {
			l.exit <- err
			return
		}
		l.read <- data[:n]
	}
}

func (l *local) Write() {
	for {
		select {
		case data := <-l.write:
			_, err := l.conn.Write(data)
			if err != nil {
				l.exit <- err
				return
			}
		}
	}
}

func main() {
	flag.Parse()

	target := net.JoinHostPort(host, fmt.Sprintf("%d", remotePort))
	for {
		serverConn, err := net.Dial("tcp", target)
		if err != nil {
			panic(err)
		}

		fmt.Printf("已连接server: %s \n", serverConn.RemoteAddr())
		server := &server{
			conn:   serverConn,
			read:   make(chan []byte),
			write:  make(chan []byte),
			exit:   make(chan error),
			reConn: make(chan bool),
		}

		go server.Read()
		go server.Write()

		go handle(server)

		<-server.reConn
		_ = server.conn.Close()
	}

}

func handle(server *server) {
	// 等待server端发来的信息，也就是说user来请求server了
	//data := <-server.read

	localConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		panic(err)
	}

	local := &local{
		conn:  localConn,
		read:  make(chan []byte),
		write: make(chan []byte),
		exit:  make(chan error),
	}

	go local.Read()
	go local.Write()

	/*与下方代码重复*/
	//local.write <- data

	for {
		select {
		case data := <-server.read:
			local.write <- data

		case data := <-local.read:
			server.write <- data

		case err := <-server.exit:
			fmt.Printf("server have err: %s", err.Error())
			_ = server.conn.Close()
			_ = local.conn.Close()
			server.reConn <- true
			return
		case err := <-local.exit:
			fmt.Printf("server have err: %s", err.Error())
			_ = local.conn.Close()
			return
		}
	}
}
