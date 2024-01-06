rm -r build
mkdir build

cd cmd/master/
env CGO_ENABLED=0 go build -trimpath
cp ./master ../../build


cd ../slave/
env CGO_ENABLED=0 go build -trimpath
cp ./slave ../../build/

cd ../..
cp -r ./docs ./build/
