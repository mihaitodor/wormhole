# Wormhole

## Introduction

Wormhole is a minimalistic [ansible](https://www.ansible.com/)-like tool.

It is designed to read a list of servers from [inventory.yaml](inventory.yaml) and run on each of them a playbook, for example [wormhole.yaml](playbooks/wormhole.yaml).

## Features

- Connections to remote servers are performed via `ssh` using username/password authentication.
- The `scp` protocol is used for copying files on remote servers.
- Early playbook cancellation via SIGINT (Ctrl+C) / SIGTERM. The application will exit almost immediately.
- Parallel execution on multiple hosts.
- Super-fast build and execution times.

## Build instructions

First, run `bootstrap.sh` to check dependencies and fetch the required version of Go. Afterwards, run `build.sh` to build and test the code. The main executable will be generated in `./wormhole`.

Please note that both `gcc` and `git` need to be installed on the system.

This project has been tested on both OSX and Ubuntu.

### Test against a Docker container

```Shell
> sudo docker run --rm -p2222:22 -p80:80 rastasheep/ubuntu-sshd:14.04

> cat inventory.yaml
---

- host: localhost
  port: 2222
  username: root
  password: root

> ./wormhole playbooks/wormhole.yaml
INFO[0000] Running playbook on servers: localhost:2222
INFO[0000] Running task [1/6] on "localhost:2222": Install Apache and PHP
INFO[0015] Running task [2/6] on "localhost:2222": Configure Apache ServerName
INFO[0015] Running task [3/6] on "localhost:2222": Configure Apache default site
INFO[0015] Running task [4/6] on "localhost:2222": Restart Apache
INFO[0017] Running task [5/6] on "localhost:2222": Copy index.php
INFO[0017] Running task [6/6] on "localhost:2222": Validate host
INFO[0017] Playbook ran successfully on servers: localhost:2222
```

### Code linter

Done via [`golangci-lint run`](https://github.com/golangci/golangci-lint).

## Usage

For typical usage, add your servers to `inventory.yaml` and run `./wormhole path/to/playbook.yaml`.

Use `./wormhole --help` to get quick information about the rest of the parameters, which are optional.

### Optional command line parameters

- `-i` - The path to the server inventory file (default `inventory.yaml`), which is a Yaml sequence, each sequence item containing the connection details of a distinct server. Example server definition:

```YAML
- host: "ec2-127-0-0-1.compute-1.amazonaws.com"
  port: 22
  username: ubuntu
  password: "Passw0rd!"
```

- `-c` - The connection timeout for the ssh connection to the remote host

- `-e` - The execution timeout for each command that will run via ssh

- `-m` - The maximum number of servers on which the playbook will be executed in parallel

### Playbooks

A playbook contains a list of named tasks that are executed in sequence on each server. Each task consists of a collection of actions.

For a detailed playbook example, please check [wormhole.yaml](playbooks/wormhole.yaml).

Currently, the following actions are implemented:

#### File action

Copies a local file, `src`, to `dest` on a remote server with the specified owner, owner group and mode. Example playbook definition:

```YAML
- name: Copy test.txt
  file:
    src:   files/test.txt
    dest:  /tmp/test.txt
    owner: root
    group: root
    mode:  "0644"
```

#### Apt action

Executes `apt-get update` and then `apt-get <install/remove> -y <package>` for each specified package on the remote server. Example playbook definition:

```YAML
- name: Install Apache and PHP
  apt:
    state: install
    pkg:
      - apache2
      - php5
```

#### Service action

Executes `service <service_name> <start/stop/restart>` on the remote server. Example playbook definition:

```YAML
- name: Restart Apache
  service:
    name: apache2
    state: restart
```

#### Shell action

Executes a shell command on the remote server. Example playbook definition:

```YAML
- name: Enable servername.conf for Apache
  shell: "a2enconf -q servername"
```

#### Validate action

Validates that a remote server can be reached on a given `port` after at most `retries` attempts. Each attempt needs to respond within the specified `timeout` with the specified `status_code` and `body_content`. Example playbook definition:

```YAML
- name: Validate host
  validate:
    scheme:       http
    port:         80
    url_path:     "/"
    retries:      3
    timeout:      3s
    status_code:  200
    body_content: "Hello, world!"
```

## TODO

- [ ] Integration tests against a Docker container
- [ ] Continuous integration using [Travis CI](https://travis-ci.org/)
- [ ] Support for ActionFileTemplate using Go's package template
- [ ] Verbose mode: Copy and print stdout and stderr from the remote process
- [ ] Better user input validation for the playbook and the inventory
- [ ] More unit tests
