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
	localPort  int
	remotePort int
)

func init() {
	flag.IntVar(&localPort, "l", 5200, "the user link port")
	flag.IntVar(&remotePort, "r", 3333, "client listen port")
}

type client struct {
	conn net.Conn
	// 数据传输通道
	read  chan []byte
	write chan []byte
	// 异常退出通道
	exit chan error
	// 重连通道
	reConn chan bool
}

// 从Client端读取数据
func (c *client) Read() {
	// 如果10秒钟内没有消息传输，则Read函数会返回一个timeout的错误
	_ = c.conn.SetReadDeadline(time.Now().Add(time.Second * 10))
	for {
		data := make([]byte, 10240)
		n, err := c.conn.Read(data)
		if err != nil && err != io.EOF {
			if strings.Contains(err.Error(), "timeout") {
				// 设置读取时间为3秒，3秒后若读取不到, 则err会抛出timeout,然后发送心跳
				_ = c.conn.SetReadDeadline(time.Now().Add(time.Second * 3))
				c.conn.Write([]byte("pi"))
				continue
			}
			fmt.Println("读取出现错误...")
			c.exit <- err
			return
		}

		// 收到心跳包,则跳过
		if data[0] == 'p' && data[1] == 'i' {
			fmt.Println("server收到心跳包")
			continue
		}
		c.read <- data[:n]
	}
}

// 将数据写入到Client端
func (c *client) Write() {
	for {
		select {
		case data := <-c.write:
			_, err := c.conn.Write(data)
			if err != nil && err != io.EOF {
				c.exit <- err
				return
			}
		}
	}
}

type user struct {
	conn net.Conn
	// 数据传输通道
	read  chan []byte
	write chan []byte
	// 异常退出通道
	exit chan error
}

// 从User端读取数据
func (u *user) Read() {
	_ = u.conn.SetReadDeadline(time.Now().Add(time.Second * 200))
	for {
		data := make([]byte, 10240)
		n, err := u.conn.Read(data)
		if err != nil && err != io.EOF {
			u.exit <- err
			return
		}
		u.read <- data[:n]
	}
}

// 将数据写给User端
func (u *user) Write() {
	for {
		select {
		case data := <-u.write:
			_, err := u.conn.Write(data)
			if err != nil && err != io.EOF {
				u.exit <- err
				return
			}
		}
	}
}

func main() {
	flag.Parse()

	defer func() {
		err := recover()
		if err != nil {
			fmt.Println(err)
		}
	}()

	clientListener, err := net.Listen("tcp", fmt.Sprintf(":%d", remotePort))
	if err != nil {
		panic(err)
	}
	fmt.Printf("监听:%d端口, 等待client连接... \n", remotePort)
	// 监听User来连接
	userListener, err := net.Listen("tcp", fmt.Sprintf(":%d", localPort))
	if err != nil {
		panic(err)
	}
	fmt.Printf("监听:%d端口, 等待user连接.... \n", localPort)

	for {
		// 有Client来连接了
		clientConn, err := clientListener.Accept()
		if err != nil {
			panic(err)
		}

		fmt.Printf("有Client连接: %s \n", clientConn.RemoteAddr())

		client := &client{
			conn:   clientConn,
			read:   make(chan []byte),
			write:  make(chan []byte),
			exit:   make(chan error),
			reConn: make(chan bool),
		}

		userConnChan := make(chan net.Conn)
		go AcceptUserConn(userListener, userConnChan)

		go HandleClient(client, userConnChan)

		<-client.reConn
		fmt.Println("重新等待新的client连接..")
	}
}

func HandleClient(client *client, userConnChan chan net.Conn) {

	go client.Read()
	go client.Write()

	close := make(chan struct{})

	for {
		select {
		/*调至handle函数接收，避免竞争*/
		//case err := <-client.exit:
		//	fmt.Printf("client出现错误, 开始重试, err: %s \n", err.Error())
		//	client.reConn <- true
		//	runtime.Goexit()

		case userConn := <-userConnChan:
			user := &user{
				conn:  userConn,
				read:  make(chan []byte),
				write: make(chan []byte),
				exit:  make(chan error),
			}
			go user.Read()
			go user.Write()

			go handle(client, user, close)
		case <-close:
			// 终止信号， client.exit 结束当前协程
			runtime.Goexit()
		}
	}
}

// 将两个Socket通道链接
// 1. 将从user收到的信息发给client
// 2. 将从client收到信息发给user
func handle(client *client, user *user, close chan struct{}) {
	for {
		select {
		case userRecv := <-user.read:
			// 收到从user发来的信息
			client.write <- userRecv
		case clientRecv := <-client.read:
			// 收到从client发来的信息
			user.write <- clientRecv

		case err := <-client.exit:
			fmt.Println("client出现错误, 关闭连接", err.Error())
			_ = client.conn.Close()
			_ = user.conn.Close()
			client.reConn <- true
			close <- struct{}{}
			// 结束当前goroutine
			runtime.Goexit()

		case err := <-user.exit:
			fmt.Println("user出现错误，关闭连接", err.Error())
			_ = user.conn.Close()
			// 结束当前goroutine
			runtime.Goexit()
		}
	}
}

// 等待user连接
func AcceptUserConn(userListener net.Listener, connChan chan net.Conn) {
	userConn, err := userListener.Accept()
	if err != nil {
		panic(err)
	}
	fmt.Printf("user connect: %s \n", userConn.RemoteAddr())
	connChan <- userConn
}
