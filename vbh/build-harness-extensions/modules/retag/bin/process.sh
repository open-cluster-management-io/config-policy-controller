#!/bin/bash

# Incoming parameters:
#   $1 - path to the charts that should be manipulated (typically multicloudhub-repo/multiclusterhub/charts)
#   $2 - snapshot date (just the date part)
#
# Required environment variables:
#   $QUAY_TOKEN - you know, the token... to quay (needs to be able to read open-cluster-management stuffs
#

quay_org=open-cluster-management

SNAPSHOT=$2
if [[ -z "$GITHUB_TOKEN" ]]
then
  echo "Please export GITHUB_TOKEN"
  exit 1
fi

echo Using $SNAPSHOT as snapshot date

orig_path=`pwd`
# Get the list of tgz files
cd $1
for f in *.tgz
do
  # For each - untar it, muck with it, tar it back up
  # Pick out just the subdirectory the tgz creates
  subdir=`tar -tf $f | grep values.yaml | awk -F"/" '{ print $1 }'`
  echo "Processing $f" - $subdir
  tar -xzf $f
  # untar happens locally, so just descend into that directory
  cd $subdir
  # Find all paths that contain the repository in question
  all_rows=`yq r --tojson values.yaml | jq -r '[path(.. | select(.repository|startswith("quay.io"))?)]'`
  if [[ "[]" = "$all_rows" ]]
  then
    echo "Oh no, there were no repos in $f!"
  fi
  # We now have an array of paths - have jq and awk parse through each
  for row in $(echo "$all_rows" | jq -c '.[]'); do
    search_path=`echo ${row} | jq -r .[]  | awk '{printf $0 ".";}'`
    search_path_name=`yq r values.yaml ${search_path}name`
    if [[ "" = "$search_path_name" ]]
    then
      echo "Oh no, there's an image with no name in $f!"
    else
      #echo search_path_name: $search_path_name
      search_path_version=`yq r values.yaml ${search_path}tag`
      #echo search_path_version: $search_path_version
      sha_value=`curl -s -X GET -H "Authorization: Bearer $QUAY_TOKEN" "https://quay.io/api/v1/repository/$quay_org/$search_path_name/tag/?onlyActiveTags=true&specificTag=$search_path_version-SNAPSHOT-$SNAPSHOT" | jq -r .tags[0].manifest_digest`
      #echo sha_value: $sha_value
      if [[ "null" = "$sha_value" ]]
      then
        echo "Oh no, there was no sha value for $quay_org/$search_path_name:$search_path_version-SNAPSHOT-$SNAPSHOT!"
      else
        echo "quay.io/$quay_org/$search_path_name@$sha_value"
        yq_command="yq w -i values.yaml ${search_path}sha $sha_value"
        #echo $yq_command
        eval "$yq_command"
      fi
    fi
  done
  cd ..
  tar -czf $f $subdir/*
  #rm -rf $subdir
  echo ""
done
