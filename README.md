# NiBLR

## Getting started

### Prerequisites

You need Python 3 and pip3 installed:

**macOS:**
```
brew install python3
```

**Debian/Ubuntu:**
```
sudo apt update && sudo apt install python3 python3-pip -y
```

**RHEL/Fedora:**
```
sudo dnf install python3 python3-pip -y
```

### Installation

1. Clone this repo
2. Install Python dependencies: `pip3 install -r requirements.txt`
3. Run `./setup.sh <number_of_exit_nodes>` to set up the initial system (installs Terraform, WireGuard, and generates keys).
4. To run the system you need to specify some flags, -o, -i, and -r. See the how to or examples.
   1. `python3 main.py -o operator.csv -i operator_ips.txt -r us-east-1`

This will build your environment in `us-east-1`.

## How to use 

This system requires a few command line utilities to work.

- Terraform 1.4.0 or newer
- Ansible
- Python Libraries
  - python-terraforom
  - ansible_runner
  - jinja2

To use this system you will need to run a few command line switches:

`-o:` csv file containing operator users
`-i:` file containing the IP Addresses in CIDR notation for operators originating IP
`-r:` AWS Region, use for example us-east-1 or us-east-2.

## File Formats

The file for operators should be in this format:

```
name,sshkey
operator1,sshkey
operator2,sshkey
```

An example would be:

```
name,sshkey
moses,ecdsa-sha2-nistp256 sshkey location
```

## TODO

## Credits

- The folks at WWHF sponsored the event that created ProxyCannon-NG back in 2018 and I think that's something that should be noted!
- The ProxyCannon-NG team with their repo found here: [https://github.com/proxycannon/proxycannon-ng](https://github.com/proxycannon/proxycannon-ng)

