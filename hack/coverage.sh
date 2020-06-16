#!/bin/bash

# The script accepts the coverage report output file as argument
# Which is generated by the go test

if [ -z "$1" ]
  then
    echo "No argument supplied"
    exit 1
fi

coverage=$(go tool cover -func $1 | grep total | awk '{print substr($3, 1, length($3)-1)}')
echo "$coverage"

result=$( bc <<< "${coverage%G} < $MIN_COVERAGE" )

if [[ $result == 1 ]]; then
  echo "Coverage is $coverage, which is less than the required minimum coverage: $MIN_COVERAGE "
  code=1
fi

go tool cover -html=$1 -o $1
echo "Please see the detailed coverage report in artifacts section in $1 file"

exit ${code:0}