#!/bin/bash
if [[ "$OSTYPE" == "darwin"* ]]; then
  find chart/ -type f -exec sed -i '' "s/0.0.0/$1/g" {} +
  find chart/values.yaml -type f -exec sed -i '' "s|controller|$2|g" {} +
  find chart/values.yaml -type f -exec sed -i '' "s|latest|$1|g" {} +
else
  find chart/ -type f -exec sed -i "s/0.0.0/$1/g" {} +
  find chart/values.yaml -type f -exec sed "s|controller|$2|g" {} +
  find chart/values.yaml -type f -exec sed -i "s|latest|$1|g" {} +
fi