// Rover VM infrastructure.
//
// One resource group holds exactly one Rover-managed VM plus its network.
// Size profiles (small | medium | large) map to a SKU + disk layout below.
// Keep the size map and defaults easy to edit — that is the main tuning surface.

@description('Azure region for all resources.')
param location string = resourceGroup().location

@description('Name of the VM (also used to derive related resource names).')
param vmName string = 'rover-vm'

@description('Admin username for SSH.')
param adminUsername string

@description('SSH public key contents (OpenSSH format).')
@secure()
param sshPublicKey string

@description('Compute family: burstable (CPU-credit), balanced (sustained CPU), or ramheavy (memory-optimized).')
@allowed([
  'burstable'
  'balanced'
  'ramheavy'
])
param family string = 'burstable'

@description('Size profile. xsmall is only offered for the burstable family.')
@allowed([
  'xsmall'
  'small'
  'medium'
  'large'
])
param size string = 'small'

@description('DNS label prefix for the public IP. Must be globally unique within the region.')
param dnsLabelPrefix string = toLower('${vmName}-${uniqueString(resourceGroup().id)}')

@description('OS disk size in GiB. Decoupled from compute size: the disk (and its data) is preserved across resizes. Can only grow, never shrink.')
@minValue(30)
param osDiskSizeGB int = 30

@description('Access for public SSH: Allow or Deny.')
@allowed([
  'Allow'
  'Deny'
])
param publicSshAccess string = 'Allow'

@description('TCP port the VM listens on for SSH. Rover uses a non-default high port (below the Linux ephemeral range) instead of 22.')
@minValue(1)
@maxValue(65535)
param sshPort int = 29472

// ---------------------------------------------------------------------------
// Compute family × size profiles. These map ONLY to a VM SKU — disk is
// intentionally independent (see osDiskSizeGB) so resizing compute never
// touches your data. Keep in sync with internal/sizes/sizes.go. The matrix is
// ragged: only burstable offers xsmall (Azure's balanced/ramheavy families
// have no sub-2-vCPU SKU). The CLI validates the family/size combo before
// deploying, so an unoffered combination never reaches here.
// ---------------------------------------------------------------------------
var vmSkus = {
  burstable: {
    xsmall: 'Standard_B2als_v2' // 2 vCPU / 4 GiB
    small: 'Standard_B2as_v2' // 2 vCPU / 8 GiB
    medium: 'Standard_B4als_v2' // 4 vCPU / 8 GiB
    large: 'Standard_B4as_v2' // 4 vCPU / 16 GiB
  }
  balanced: {
    small: 'Standard_D2as_v7' // 2 vCPU / 8 GiB
    medium: 'Standard_D4as_v7' // 4 vCPU / 16 GiB
    large: 'Standard_D8as_v7' // 8 vCPU / 32 GiB
  }
  ramheavy: {
    small: 'Standard_E2as_v7' // 2 vCPU / 16 GiB
    medium: 'Standard_E4as_v7' // 4 vCPU / 32 GiB
    large: 'Standard_E8as_v7' // 8 vCPU / 64 GiB
  }
}

var vmSize = vmSkus[family][size]

// Stable derived names.
var nsgName = '${vmName}-nsg'
var vnetName = '${vmName}-vnet'
var subnetName = 'default'
var pipName = '${vmName}-pip'
var nicName = '${vmName}-nic'

// cloud-init is baked into the deployment so first-boot prep always ships with
// the VM. customData must be base64-encoded. The SSH port is templated in (the
// cloud-init file uses the __ROVER_SSH_PORT__ placeholder) so the VM comes up
// already listening on the configured port — no mid-provision port switch.
var customData = base64(replace(loadTextContent('../cloud-init/cloud-init.yaml'), '__ROVER_SSH_PORT__', string(sshPort)))

resource nsg 'Microsoft.Network/networkSecurityGroups@2023-11-01' = {
  name: nsgName
  location: location
  properties: {
    securityRules: [
      {
        name: 'allow-ssh'
        properties: {
          priority: 1000
          direction: 'Inbound'
          access: publicSshAccess
          protocol: 'Tcp'
          sourcePortRange: '*'
          sourceAddressPrefix: '*'
          destinationPortRange: string(sshPort)
          destinationAddressPrefix: '*'
        }
      }
    ]
  }
}

resource vnet 'Microsoft.Network/virtualNetworks@2023-11-01' = {
  name: vnetName
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: ['10.10.0.0/16']
    }
    subnets: [
      {
        name: subnetName
        properties: {
          addressPrefix: '10.10.1.0/24'
          networkSecurityGroup: {
            id: nsg.id
          }
        }
      }
    ]
  }
}

resource pip 'Microsoft.Network/publicIPAddresses@2023-11-01' = {
  name: pipName
  location: location
  sku: {
    name: 'Standard'
  }
  properties: {
    publicIPAllocationMethod: 'Static'
    dnsSettings: {
      domainNameLabel: dnsLabelPrefix
    }
  }
}

resource nic 'Microsoft.Network/networkInterfaces@2023-11-01' = {
  name: nicName
  location: location
  properties: {
    ipConfigurations: [
      {
        name: 'ipconfig1'
        properties: {
          subnet: {
            id: vnet.properties.subnets[0].id
          }
          privateIPAllocationMethod: 'Dynamic'
          publicIPAddress: {
            id: pip.id
          }
        }
      }
    ]
  }
}

resource vm 'Microsoft.Compute/virtualMachines@2023-09-01' = {
  name: vmName
  location: location
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    hardwareProfile: {
      vmSize: vmSize
    }
    osProfile: {
      computerName: vmName
      adminUsername: adminUsername
      customData: customData
      linuxConfiguration: {
        disablePasswordAuthentication: true
        ssh: {
          publicKeys: [
            {
              path: '/home/${adminUsername}/.ssh/authorized_keys'
              keyData: sshPublicKey
            }
          ]
        }
      }
    }
    storageProfile: {
      imageReference: {
        publisher: 'Canonical'
        offer: 'ubuntu-24_04-lts'
        sku: 'server'
        version: 'latest'
      }
      osDisk: {
        name: '${vmName}-osdisk'
        createOption: 'FromImage'
        diskSizeGB: osDiskSizeGB
        managedDisk: {
          storageAccountType: 'StandardSSD_LRS'
        }
      }
    }
    networkProfile: {
      networkInterfaces: [
        {
          id: nic.id
        }
      ]
    }
  }
}

// ---------------------------------------------------------------------------
// Allow the VM to deallocate itself via rover-halt (uses IMDS + managed
// identity). Scoped to this resource group with the built-in Virtual Machine
// Contributor role — broad enough to start/stop/deallocate, but the RG only
// holds this one VM so the blast radius is minimal. The role assignment is
// implicitly dependent on the VM (Bicep sees vm.identity.principalId).
// ---------------------------------------------------------------------------
resource vmSelfDeallocate 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(vm.id, 'rover-vm-contributor')
  properties: {
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '9980e02c-c2be-4d73-94e8-173b1dc7cf3c')
    principalId: vm.identity.principalId
    principalType: 'ServicePrincipal'
  }
}

// ---------------------------------------------------------------------------
// Outputs consumed by scripts / the Rover CLI.
// ---------------------------------------------------------------------------
output vmName string = vm.name
output resourceGroup string = resourceGroup().name
output location string = location
output adminUsername string = adminUsername
output vmSize string = vmSize
output osDiskSizeGB int = osDiskSizeGB
output sshPort int = sshPort
output publicIp string = pip.properties.ipAddress
output fqdn string = pip.properties.dnsSettings.fqdn
output privateIp string = nic.properties.ipConfigurations[0].properties.privateIPAddress
output sshTarget string = '${adminUsername}@${pip.properties.dnsSettings.fqdn}'
