# NiBLR

## Getting started

1. Clone this repo
2. run `pip3 install -r requirements.txt`
3. Currently we have pinned the argparse to require specific settings so run the `./setup.sh` manually to setup the initial system.
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

