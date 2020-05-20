import json
import sys
import os
from subprocess import run

# Parameters:
#  sys.argv[1] - filename of manifest.json to parse
#  sys.argv[2] - textual tag
#  sys.argv[3] - boolean: dry run/validate (true) vs. actually do the tagging (false)
#  sys.argv[4] - textual name of repo to do the tagging in (git|quay)

data = json.load(open(sys.argv[1]))
for v in data:
    try:
        # First, try manifest keys v1
        component_name = v["name"]
        compenent_tag = v["tag"]
        compenent_sha = v["sha256"]
        component_repo = v["repository"]
        component_version = v["tag"].replace('-'+v["sha256"],'')
    except:
        # If that doesn't work, try manifest keys v2
        component_name = v["image-name"]
        compenent_tag = v["image-tag"]
        compenent_sha = v["git-sha256"]
        component_repo = v["git-repository"]
        component_version = v["image-tag"].replace('-'+v["git-sha256"],'')
    if (sys.argv[2].startswith('20')):
        # Assuming a date is coming in as a tag, mark it all up with SNAPSHOT decoration specifc to this repo
        retag_name = component_version + "-SNAPSHOT-" + sys.argv[2]
    else:
        # Tag is respected as literally and solely the second argument contents, likely a version/release tag
        retag_name = sys.argv[2]
    if (component_repo.startswith('open-cluster-management')):
        # We don't try to tag anyone else's repos
        run('echo RETAG_SNAPSHOT_NAME={} COMPONENT_NAME={} RETAG_REPO={} RETAG_QUAY_COMPONENT_TAG={} RETAG_GITHUB_SHA={} RETAG_DRY_RUN={} repo-type={}'.format(retag_name, component_repo, component_name, compenent_tag, compenent_sha, sys.argv[3], sys.argv[4]), shell=True)
        if (sys.argv[4] == "git"):
            run('make retag/git RETAG_SNAPSHOT_NAME={} RETAG_REPO={} RETAG_QUAY_COMPONENT_TAG={} RETAG_GITHUB_SHA={} RETAG_DRY_RUN={}'.format(retag_name, component_repo, compenent_tag, compenent_sha, sys.argv[3]), shell=True, check=True)
        elif (sys.argv[4] == "quay"):
            run('make retag/quay RETAG_SNAPSHOT_NAME={} COMPONENT_NAME={} RETAG_QUAY_COMPONENT_TAG={} RETAG_GITHUB_SHA={} RETAG_DRY_RUN={}'.format(retag_name, component_name, compenent_tag, compenent_sha, sys.argv[3]), shell=True, check=True)
