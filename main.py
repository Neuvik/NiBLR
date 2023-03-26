#!/usr/bin/env python3

import argparse
import os.path
import logging
import csv
import ansible_runner 
import time

from python_terraform import *
from jinja2 import Environment, FileSystemLoader

LOGGING_DIRECTORY="logging"
LOGGING_FILE="nipr.log"

PROVIDERS_TF="providers.tf"
PROVIDERS_TEMPLATE="providers.jinja"

VARIABLES_TF="variables.tf"
VARIABLES_TEMPLATE="variables.jinja"

TERRAFORM_DIRECTORY="terraform"
TEMPLATE_DIRECTORY="templates"

logging_path=os.path.join(LOGGING_DIRECTORY, LOGGING_FILE)
logging.basicConfig(format='%(asctime)s %(message)s', filename=logging_path, level=logging.INFO)


def generate_providers(args):
    if args.azure == True:
        azure_template = """
    azurerm = {
      source = "hashicorp/azurerm"
      version = "=3.13.0"
    }
        """
    else:
        azure_template = ""

    file_loader = FileSystemLoader(TEMPLATE_DIRECTORY)
    env = Environment(loader=file_loader)

    template = env.get_template(PROVIDERS_TEMPLATE)
    provider_output = template.render(
        AZURE_PROVIDER = azure_template
    )
    
    providers_full_path = os.path.join(TERRAFORM_DIRECTORY, PROVIDERS_TF)
    with open(providers_full_path, "w") as f:
        f.write(provider_output)

    logging.info(f"[+] Creating the providers terraform file: {providers_full_path}")
    print(f"[+] Creating the providers terraform file: {providers_full_path}")

def generate_variables(args):

    file_loader = FileSystemLoader(TEMPLATE_DIRECTORY)
    env = Environment(loader=file_loader)

    operators_dictionary = {}
    operators_list = []
    
    if not os.path.exists(args.operators):
        print(f"File doesn't exist: {args.operators}")
        print(f"Going to exit")
        return False

    with open(args.operators, "r") as r:
        csvreader=csv.reader(r)
        next(csvreader)

        for row in csvreader:
            key = row[0]
            value = row[1]
            
            operators_dictionary[key] = value

        operators_list.append(operators_dictionary)
    r.close()

    operators_ips = []
    with open(args.ips, "r") as o:
        operator_ips_list = [line.rstrip() for line in o]
        operator_ips = "\", \n\"".join(map(str, operator_ips_list))

    template = env.get_template(VARIABLES_TEMPLATE)
    template_output = template.render(
        OPERATORS_LIST = operators_list,
        OPERATOR_IPS = operator_ips,
        AWS_REGION = args.aws_region,
    )

    variables_full_path = os.path.join(TERRAFORM_DIRECTORY, VARIABLES_TF)
    with open(variables_full_path, "w") as f:
        f.write(template_output)

    logging.info(f"[+] Creating the variables terraform file: {variables_full_path}")
    print(f"[+] Creating the variables terraform file: {variables_full_path}")


def main():

    parser = argparse.ArgumentParser(description='The inspiration for this comes from ProxyCannon-ng. We wanted to provide a new way to rotate through multiple providers and a salient script. Enjoy - @mosesrenegade')

    # Add argument for name for enabling azure 
    parser.add_argument('-a', '--azure', action='store_true', help="Enable Azure, default is disabled", default=False)

    # Add argument for operators keys
    parser.add_argument('-o', '--operators', dest='operators', help="Please add a file with a list of operators to be used to login to the system", required=True)

    # Add argument for operators ips
    parser.add_argument('-i', '--ips', dest='ips', help="Please add a file with a list of operators ips to be used to login to the system", required=True)

    parser.add_argument('-r', '--awsregion', dest='aws_region', help="AWS region, supported regions are defined by the AWS API", required=True)
    # parse arguments
    args = parser.parse_args()
    
    logging.info(f"[+] Going into generator providers now")
    generate_providers(args)

    logging.info(f"[+] Going into generator variables now")
    generate_variables(args)

    #os.system("cd terraform; terraform apply -auto-approve")
    try:
        tf = Terraform(working_dir='./terraform')
        print(f"[+] Running terraform fmt")
        return_code, stdout, stderr = tf.fmt(diff=True)
        logging.info(f"[+] Ran terraform fmt and returned {return_code}")
        
        print(f"[+] Running terraform apply")
        return_code, stdout, stderr = tf.apply(no_color=IsFlagged,input=False,auto_approve=True)
        logging.info(f"[+] Ran terraform apply and returned {return_code}")

        os.system("cd terraform; terraform output -raw client_conf > ../openvpn_client.conf")

    except Exception as e:
        logging.info(e)

    print(f"""
[-] We now have the system built but it will take a few minutes for this system to be available and for ansible to control it.
[-] Let's use a countdown timer for 5 minutes to fix this.
""")
    
    t = 1
    while t:
        mins, secs = divmod(t, 60)
        timer = '{:02d}:{:02d}'.format(mins, secs)
        print(timer, end="\r")
        time.sleep(1)
        t -= 1

    try:
        ansiblerun = ansible_runner.run(private_data_dir='./ansible', playbook='playbook.yml')
        print(f"Running ansible now, the status is: {ansiblerun.status}: {ansiblerun.rc}")
        logging.info(f"Running ansible now, the status is: {ansiblerun.status}: {ansiblerun.rc}")

    except Exception as e:
        logging.info(e)



if __name__ == "__main__":
    try:
        print(f"""
NIBLR
Neuviks' Ip BLock Rotator

Thank you to the great work by the ProxyCannon + ProxyCannon-ng folks, this needed an update

Do good with this. - @mosesrenegade and the team at Neuvik

... Go watch Futureama.
""")
        main()

    except Exception as e:
        logging.info(e)