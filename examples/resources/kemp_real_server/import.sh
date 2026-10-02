# Real servers are imported by <virtual service or SubVS id>/rs/<index>.
# <current virtual service index>/<real server index> also works.
terraform import kemp_real_server.web tcp/10.0.253.50/443/rs/5
