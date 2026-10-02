# SubVSs are imported by <parent id>/sub/<slot>, where slot is the SubVS's
# real server index on the parent. Its current virtual service index also
# works, and is converted to that reference.
terraform import kemp_sub_virtual_service.cluster_a tcp/10.0.253.2/443/sub/2
