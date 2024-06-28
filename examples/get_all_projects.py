import os
import re
import subprocess
from subprocess import Popen

try:
    ACCOUNT_ID = os.environ['DBT_CLOUD_ACCOUNT_ID']
except KeyError:
    raise KeyError(f"Please set the DBT_CLOUD_ACCOUNT_ID by running 'export DBT_CLOUD_ACCOUNT_ID=<redacted>`")

try:
    API_TOKEN = os.environ['DBT_CLOUD_TOKEN']
except KeyError:
    raise KeyError(f"Please set the DBT_CLOUD_TOKEN by running 'export DBT_CLOUD_TOKEN=<redacted>`")

try:
    API_URL = os.environ['DBT_CLOUD_HOST_URL']
except KeyError:
    raise KeyError(f"Please set the DBT_CLOUD_HOST_URL by running 'export DBT_CLOUD_HOST_URL=<redacted>`")

bash_cmd = f"""
dbtcloud-terraforming generate --account {ACCOUNT_ID} --token {API_TOKEN} --host-url {API_URL} --resource-types dbtcloud_project --linked-resource-types='all'
"""
try:
    output = subprocess.check_output(bash_cmd, shell=True)
except subprocess.CalledProcessError as e:
    print(f"Error while executing command: {e}")
    print(f"Command: {bash_cmd}")
    raise Exception("Unable to generate the configuration files. Please check the error message above.")

project_pattern = re.compile(
    r"resource\s*\"dbtcloud_project\"\s*\"terraform_managed_resource_(\d+)\"\s*{\s*name\s*=\s*\"(.+)\"\s*}"
)

matches = project_pattern.findall(output.decode('utf-8'))

projects = []
for match in matches:
    project = {
        "id": match[0],
        "name": match[1]
    }
    projects.append(project)

for i in projects[2:4]:
    # Execute a bash command
    project_id = i["id"]
    project_name = i["name"].replace(" ", "_")
    options = ["dbtcloud_project", "dbtcloud_environment", "dbtcloud_job", "dbtcloud_repository",
               "dbtcloud_bigquery_connection", "dbtcloud_bigquery_credential", "dbtcloud_project_repository",
               "dbtcloud_project_connection", "dbtcloud_postgres_credential", "dbtcloud_environment_variable",
               "dbtcloud_connection"]
    # First we create the file and add the private key variable to it
    with open(f"project_{project_name.lower()}.tf", "w") as f:
        f.write(f"""variable "{project_name.lower()}_private_key" {{
  description = "The private key for the service account"
  type        = string
}}

variable "{project_name.lower()}_private_key_id" {{
  description = "The private key ID for the service account"
  type        = string
}}

""")

    for option in options:
        bash_cmd = f"""
        dbtcloud-terraforming generate --account {ACCOUNT_ID} --token {API_TOKEN} --host-url {API_URL} --resource-types {option} --linked-resource-types='all' --projects={project_id} >> project_{project_name.lower()}.tf
        """
        try:
            print(f"Executing command: {bash_cmd}")
            process = Popen(bash_cmd, shell=True, executable='/bin/bash')
            process.wait()
        except subprocess.CalledProcessError as e:
            print(f"Error while executing command: {e}")
            print(f"Command: {bash_cmd}")

    print(f"Project {project_name} has been loaded")
