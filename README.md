# HBICHAT

Showcase and stress-test [HBI](https://github.com/complyue/hbi) in all supported languages.

HBICHAT is currently a bilingual (Python/Golang) project, i.e. with same functionalities
and API/protocol implemented in both languages.
It shall evolve to be trilingual (with es6 added) later, but not sooner.

## Getting HBICHAT

```console
cyue@cyuembpx:~$ go get -u github.com/complyue/hbichat
```

## Running HBICHAT Server

### Start Golang based server

```console
cyue@cyuembpx:~$ cd /dev/shm
cyue@cyuembpx:/dev/shm$ go run github.com/complyue/hbichat/cmd/server localhost:3232
I0527 01:49:02.496624   10966 main.go:44] HBI chat service listening: 127.0.0.1:3232
```

### Start Python based server

```console
cyue@cyuembpx:~$ cd /dev/shm
cyue@cyuembpx:/dev/shm$ export PYTHONPATH=$(dirname $(go list -f '{{.Dir}}' github.com/complyue/hbichat))
cyue@cyuembpx:/dev/shm$ python -m hbichat.cmd.server localhost:3232
[2019-05-27T01:50:35 11008](hbichat.cmd.server) HBI Chatting Server listening:
  * 127.0.0.1:3232
 -INFO- File "/home/cyue/m3works/go-devs/src/github.com/complyue/hbichat/cmd/server/__main__.py", line 112, in serve_chatting
```

## Running HBICHAT Client

### Start Golang based client

```console
cyue@cyuembpx:~$ cd /dev/shm
cyue@cyuembpx:/dev/shm$ go run github.com/complyue/hbichat/cmd/client localhost:3232

@@ Welcome Stranger$127.0.0.1:54960, this is chat service at 127.0.0.1:3232 !
 -
@@ There're 1 room(s) open, and you are in #Lobby now.

  -*-	0 chatter(s) in room #Lobby
Stranger$127.0.0.1:54960@127.0.0.1:3232#Lobby: ?

Usage:

 # _room_
    goto a room

 $ _nick_
    change nick

 .
    list local files

 ^
    list server files

 > _file-name_
    upload a file

 < _file-name_
    download a file

!
    quit with stacktraces of all goroutines dumped

* [ _n_bots_=10 ] [ _n_rooms_=10 ] [ _n_msgs_=10 ] [ _n_files_=10 ] [ _file_max_kb_=1234 ] [ _file_min_kb_=2 ]
    spam the service for stress-test

Stranger$127.0.0.1:54960@127.0.0.1:3232#Lobby:

```

### Start Python based client

```console
cyue@cyuembpx:~$ cd /dev/shm
cyue@cyuembpx:/dev/shm$ export PYTHONPATH=$(dirname $(go list -f '{{.Dir}}' github.com/complyue/hbichat))
cyue@cyuembpx:/dev/shm$ python -m hbichat.cmd.client localhost:3232

@@ Welcome Stranger$127.0.0.1:54972, this is chat service at 127.0.0.1:3232 !
 -
@@ There're 1 room(s) open, and you are in #Lobby now.
  -*-	0 chatter(s) in room #Lobby

Stranger$127.0.0.1:54972@127.0.0.1:3232#Lobby: ?

Usage:

 # _room_
    goto a room

 $ _nick_
    change nick

 .
    list local files

 ^
    list server files

 > _file-name_
    upload a file

 < _file-name_
    download a file

!
    dump stacktraces of all asyncio tasks

* [ _n_bots_=10 ] [ _n_rooms_=10 ] [ _n_msgs_=10 ] [ _n_files_=10 ] [ _file_max_kb_=1234 ] [ _file_min_kb_=2 ]
    spam the service for stress-test


Stranger$127.0.0.1:54972@127.0.0.1:3232#Lobby:

```
