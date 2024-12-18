# Build and push bundle image to local registry
cd test/fake-operator-test/fake-operator
make bundle >/dev/null 
make bundle-build >/dev/null 
make bundle-push >/dev/null
make docker-build >/dev/null
make docker-push >/dev/null

# Build and push catalog bundle image to local registry
cd ..
docker build . \
    -f fake-catalog.Dockerfile \
    -t localhost:5001/fake-catalog:latest >/dev/null \

docker push localhost:5001/fake-catalog:latest >/dev/null

# Apply the catalog to the cluster
kubectl apply -f fake-catalog.yaml

# Apply controller-manager to the cluster
# NOTE: This is a workaround since the SA doesn't seem to be automatically applied.
# regardless of this, the deployment will still fail due to unknown reasons.
cd fake-operator/config/rbac
kubectl apply -f service_account.yaml