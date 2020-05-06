#!/bin/bash

# Incoming variables:
#   $1 - name of the manifest json file (should exist)
#   $2 - name of the sha'd manifest json file (to be created)
#   $3 - image-to-alias dictionary (image-alias.json, should exist)
#
# Required environment variables:
#   $QUAY_TOKEN - you know, the token... to quay (needs to be able to read open-cluster-management stuffs
#

if [[ -z "$QUAY_TOKEN" ]]
then
  echo "Please export QUAY_TOKEN"
  exit 1
fi

manifest_filename=$1
new_filename=$2
dictionary_filename=$3

echo Incoming manfiest filename: $manifest_filename
echo Creating shad manfiest filename: $new_filename

rm manifest-sha.badjson 2> /dev/null
cat $manifest_filename | jq -rc '.[]' | while IFS='' read item;do
  name=$(echo $item | jq -r '.["image-name"]')
  remote=$(echo $item | jq -r '.["image-remote"]')
  repository=$(echo $item | jq -r '.["image-remote"]' | awk -F"/" '{ print $2 }')
  tag=$(echo $item | jq -r '.["image-tag"]')
  image_key=$(jq -r --arg image_name $name '.[] | select (.["image-name"]==$image_name) | .["image-key"]' $dictionary_filename )
  # echo image name: [$name] remote: [$remote] repostory: [$repository] tag: [$tag] image_key: [$image_key]
  url="https://quay.io/api/v1/repository/$repository/$name/tag/?onlyActiveTags=true&specificTag=$tag"
  # echo $url
  curl_command="curl -s -X GET -H \"Authorization: Bearer $QUAY_TOKEN\" \"$url\""
  #echo $curl_command
  sha_value=$(eval "$curl_command | jq -r .tags[0].manifest_digest")
  echo sha_value: $sha_value
  if [[ "null" = "$sha_value" ]]
  then
    echo Oh no, can\'t retrieve sha from $url
    exit 1
  fi
  echo $item | jq --arg sha_value $sha_value --arg image_key $image_key '. + { "image-digest": $sha_value, "image-key": $image_key }' >> manifest-sha.badjson
done
echo Creating $new_filename file
jq -s '.' < manifest-sha.badjson > $new_filename
rm manifest-sha.badjson 2> /dev/null
