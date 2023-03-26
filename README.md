# NiBLR

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
