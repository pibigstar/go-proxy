package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"time"
)

var (
	localPort  int
	remotePort int
	reConnChan = make(chan bool)
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
}

func (c *client) Read() {
	_ = c.conn.SetReadDeadline(time.Now().Add(time.Second * 5))
	for {
		data := make([]byte, 10240)

		n, err := c.conn.Read(data)
		if err != nil && err != io.EOF {
			c.exit <- err
			reConnChan <- true
		}
		c.read <- data[:n]
	}
}

func (c *client) Write() {
	for {
		select {
		case data := <-c.write:
			_, err := c.conn.Write(data)
			if err != nil && err != io.EOF {
				c.exit <- err
				reConnChan <- true
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

func (u *user) Read() {
	_ = u.conn.SetReadDeadline(time.Now().Add(time.Second * 200))
	for {
		data := make([]byte, 10240)
		n, err := u.conn.Read(data)
		if err != nil && err != io.EOF {
			u.exit <- err
			reConnChan <- true
		}
		u.read <- data[:n]
	}
}

func (u *user) Write() {
	for {
		select {
		case data := <-u.write:
			_, err := u.conn.Write(data)
			if err != nil && err != io.EOF {
				u.exit <- err
				reConnChan <- true
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

	userListener, err := net.Listen("tcp", fmt.Sprintf(":%d", localPort))
	if err != nil {
		panic(err)
	}
	fmt.Printf("监听:%d端口, 等待user连接.... \n", localPort)

	userConnChan := make(chan net.Conn)
	go AcceptUserConn(userListener, userConnChan)

	// 有Client来连接了
	clientConn, err := clientListener.Accept()
	if err != nil {
		panic(err)
	}

	client := &client{
		conn:  clientConn,
		read:  make(chan []byte),
		write: make(chan []byte),
		exit:  make(chan error),
	}

	go client.Read()
	go client.Write()

	for {
		select {
		case userConn := <-userConnChan:
			user := &user{
				conn:  userConn,
				read:  make(chan []byte),
				write: make(chan []byte),
				exit:  make(chan error),
			}
			go handle(client, user)
		case <-reConnChan:
			fmt.Println("重新监听连接")
			go AcceptUserConn(userListener, userConnChan)
		}
	}

}

// 将两个Socket通道链接
// 1. 将从user收到的信息发给client
// 2. 将从client收到信息发给user
func handle(client *client, user *user) {
	for {
		select {
		case userRecv := <-user.read:
			fmt.Println("收到从user发来的信息")
			client.write <- userRecv
		case clientRecv := <-client.read:
			fmt.Println("收到从client发来的信息")
			user.write <- clientRecv
		case err := <-client.exit:
			fmt.Println(err.Error())
			_ = client.conn.Close()
			_ = user.conn.Close()
		case err := <-user.exit:
			fmt.Println(err.Error())
			_ = client.conn.Close()
			_ = user.conn.Close()
		}
	}
}

// 等待user连接
func AcceptUserConn(userListener net.Listener, connChan chan net.Conn) {
	userConn, err := userListener.Accept()
	if err != nil {
		panic(err)
	}
	fmt.Println("有user来连接...")
	connChan <- userConn
}
