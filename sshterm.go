package sshterm

import (
	"encoding/binary"
	"fmt"
	"net"

	"golang.org/x/crypto/ssh"

	tb "github.com/andyleap/SSHTerm/SSHTermbox"
)

/*
func NewTermServer()
	config := &ssh.ServerConfig{
		//Define a function to run when a client attempts a password login
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			// Should use constant-time compare (or better, salt+hash) in a production setting.
			var user User
			NoUsers := false
			err := db.View(func(tx *bolt.Tx) error {
				users := tx.Bucket([]byte("Users"))
				if users == nil {
					NoUsers = true
					return nil
				}
				rawUser := users.Get([]byte(c.User()))
				if rawUser == nil {
					return fmt.Errorf("password rejected for %q", c.User())
				}
				json.Unmarshal(rawUser, &user)
				return nil
			})
			if err != nil {
				return nil, err
			}
			if NoUsers {
				return nil, nil
			}
			if c.User() == user.Name && bcrypt.CompareHashAndPassword(user.Password, pass) == nil {
				log.Printf("User %s logged in", c.User())
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %q", c.User())
		},
	}

	// You can generate a keypair with 'ssh-keygen -t rsa'
	privateBytes, err := ioutil.ReadFile("id_rsa")
	if err != nil {
		pk, _ := rsa.GenerateKey(rand.Reader, 2048)
		pkBytes := x509.MarshalPKCS1PrivateKey(pk)
		b := &pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: pkBytes,
		}
		privateBytes = pem.EncodeToMemory(b)

		ioutil.WriteFile("id_rsa", privateBytes, 0777)
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		log.Fatal("Failed to parse private key")
	}

	config.AddHostKey(private)

	// Once a ServerConfig has been configured, connections can be accepted.
	listener, err := net.Listen("tcp", "0.0.0.0:2200")
	if err != nil {
		log.Fatalf("Failed to listen on 2200 (%s)", err)
	}

	// Accept all connections
	log.Print("Listening on 2200...")
	for {
		tcpConn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed to accept incoming connection (%s)", err)
			continue
		}
		// Before use, a handshake must be performed on the incoming net.Conn.
		sshConn, chans, reqs, err := ssh.NewServerConn(tcpConn, config)
		if err != nil {
			log.Printf("Failed to handshake (%s)", err)
			continue
		}

		log.Printf("New SSH connection from %s (%s)", sshConn.RemoteAddr(), sshConn.ClientVersion())
		// Discard all global out-of-band Requests
		go ssh.DiscardRequests(reqs)
		// Accept all channels
		go handleChannels(chans)
	}
}*/

type Term interface {
	Resize(w, h int)
}

type TermServer struct {
	Config  *ssh.ServerConfig
	Handler func(tb *tb.Termbox) Term
}

func New(conf *ssh.ServerConfig) *TermServer {
	return &TermServer{
		Config: conf,
	}
}

func (ts *TermServer) Listen(l net.Listener) {
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			continue
		}
		_, chans, reqs, err := ssh.NewServerConn(tcpConn, ts.Config)
		if err != nil {
			continue
		}
		go ssh.DiscardRequests(reqs)
		go ts.handleChannels(chans)
	}
}

func (ts *TermServer) handleChannels(chans <-chan ssh.NewChannel) {
	// Service the incoming Channel channel in go routine
	for newChannel := range chans {
		go ts.handleChannel(newChannel)
	}
}

type ptyReq struct {
	Term    string
	Width   uint32
	Height  uint32
	PWidth  uint32
	PHeight uint32
	Modes   string
}

type envVar struct {
	Name  string
	Value string
}

type subsystemReq struct {
	Name string
}

func (ts *TermServer) handleChannel(newChannel ssh.NewChannel) {
	// Since we're handling a shell, we expect a
	// channel type of "session". The also describes
	// "x11", "direct-tcpip" and "forwarded-tcpip"
	// channel types.
	if t := newChannel.ChannelType(); t != "session" {
		newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
		return
	}

	// At this point, we have the opportunity to reject the client's
	// request for another logical connection
	connection, requests, err := newChannel.Accept()
	if err != nil {
		return
	}

	var term Term

	// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
	go func() {
		for req := range requests {
			switch req.Type {
			case "subsystem":
				if req.WantReply {
					req.Reply(false, nil)
				}
			case "shell":
				// We only accept the default shell
				// (i.e. no command in the Payload)
				if len(req.Payload) == 0 {
					req.Reply(true, nil)
				}
			case "env":
			case "pty-req":
				var pty ptyReq
				ssh.Unmarshal(req.Payload, &pty)

				// Responding true (OK) here will let the client
				// know we have a pty ready for input

				t, _ := tb.Init(connection, connection, pty.Term, int(pty.Width), int(pty.Height))

				term = ts.Handler(t)

				req.Reply(true, nil)
			case "window-change":
				w, h := parseDims(req.Payload)
				if term != nil {
					term.Resize(int(w), int(h))
				}
			}
		}
	}()
}

// =======================

// parseDims extracts terminal dimensions (width x height) from the provided buffer.
func parseDims(b []byte) (uint32, uint32) {
	w := binary.BigEndian.Uint32(b)
	h := binary.BigEndian.Uint32(b[4:])
	return w, h
}
