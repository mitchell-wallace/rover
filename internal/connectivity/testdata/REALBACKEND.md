# Real-backend verification notes

Update when re-verifying against live Azure.

Last verified: 2026-06-10 against Azure subscription in australiaeast.

## Azure responses captured from `az` CLI

`vm_power_state` (deallocated):

```text
az vm get-instance-view -g rover-rg -n rover-vm \
  --query "instanceView.statuses[?starts_with(code,'PowerState/')].displayStatus|[0]" -o tsv
→ "VM deallocated"
```

`vm_power_state` (running):

```text
→ "VM running"
```

`vm show` (SKU):

```text
az vm show -g rover-rg -n rover-vm --query hardwareProfile.vmSize -o tsv
→ "Standard_B2als_v2"
```

NSG rule:

```text
az network nsg rule show -g rover-rg --nsg-name rover-vm-nsg -n allow-ssh \
  --query access -o tsv
→ "Deny" (when Tailscale lockdown active)
→ "Allow" (when public SSH open)
```

## Tailscale responses captured from `tailscale` CLI

`tailscale status --json` (peer shape, online):

```json
{
  "HostName": "rover-vm",
  "DNSName": "rover-vm.tail94a70e.ts.net.",
  "Online": true,
  "TailscaleIPs": ["100.88.25.46"]
}
```

`tailscale status --json` (peer shape, offline after deallocation):

```json
{ "Online": false }
```

Inside VM after deallocation+restart:

```text
tailscale status → "Logged out.\nLog in at: https://login.tailscale.com/a/...\n"
```

## Bicep deployment error when redeploying existing VM

```json
{"error":{"code":"PropertyChangeNotAllowed","message":"Changing property 'osProfile.customData' is not allowed."}}
```
