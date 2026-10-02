# Virtual services are imported by <protocol>/<address>/<port>. Their current
# LoadMaster index also works, and is converted to that reference.
terraform import kemp_virtual_service.web tcp/10.0.253.50/443
