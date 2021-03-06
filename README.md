# sbc-demo-backend
One Daijo's Demo Backend

Written in Go

This takes advantage of two GCP services: Firebase and Cloud Datatstore.

The primary backend code is under `server/`.
`server/run_rest_server.sh` (needs to be run from inside `server/`) is the main run script.
To run this backend, you'll need:
1.  GCP credentials stored in `server/cloud_credentials.json`.
2.  Your stellar seed stored in `server/stellar_seed.txt`.
3.  A certificate and private key: `server/server.crt`, `server/server.key`.
4.  A configured environment for building Go projects with dependencies.

Other Go scripts that we found useful are available in other folders.

To build and run the server (you may need root to bind to port 443):
```
cd server
./run_rest_server.sh
```
