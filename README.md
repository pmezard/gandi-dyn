# What is it?

Dynamic IPs are a pain for self-hosting but many providers still use them. Some
of them even do it on IPv6. Yeah really, kudos Orange.

This Go tool checks the current IP using an external service and update Gandi A
records with their API. Use it like:

```
$ gandi-dyn apikey mydomain.org
```

