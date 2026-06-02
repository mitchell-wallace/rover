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

@description('Size profile.')
@allowed([
  'small'
  'medium'
  'large'
])
param size string = 'small'

@description('DNS label prefix for the public IP. Must be globally unique within the region.')
param dnsLabelPrefix string = toLower('${vmName}-${uniqueString(resourceGroup().id)}')

// ---------------------------------------------------------------------------
// Size profiles. Edit these to change the machine envelope per size.
// ---------------------------------------------------------------------------
var sizeProfiles = {
  small: {
    vmSize: 'Standard_B2ls_v2' // 2 vCPU / 4 GiB
    osDiskSizeGB: 30
    dataDiskSizeGB: 0
  }
  medium: {
    vmSize: 'Standard_B2s_v2' // 2 vCPU / 8 GiB
    osDiskSizeGB: 30
    dataDiskSizeGB: 0
  }
  large: {
    vmSize: 'Standard_B4s_v2' // 4 vCPU / 16 GiB
    osDiskSizeGB: 64
    dataDiskSizeGB: 0
  }
}

var profile = sizeProfiles[size]
var hasDataDisk = profile.dataDiskSizeGB > 0

// Stable derived names.
var nsgName = '${vmName}-nsg'
var vnetName = '${vmName}-vnet'
var subnetName = 'default'
var pipName = '${vmName}-pip'
var nicName = '${vmName}-nic'

// cloud-init is baked into the deployment so first-boot prep always ships with
// the VM. customData must be base64-encoded.
var customData = loadFileAsBase64('../cloud-init/cloud-init.yaml')

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
          access: 'Allow'
          protocol: 'Tcp'
          sourcePortRange: '*'
          sourceAddressPrefix: '*'
          destinationPortRange: '22'
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
  properties: {
    hardwareProfile: {
      vmSize: profile.vmSize
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
        diskSizeGB: profile.osDiskSizeGB
        managedDisk: {
          storageAccountType: 'StandardSSD_LRS'
        }
      }
      dataDisks: hasDataDisk ? [
        {
          name: '${vmName}-datadisk'
          lun: 0
          createOption: 'Empty'
          diskSizeGB: profile.dataDiskSizeGB
          managedDisk: {
            storageAccountType: 'StandardSSD_LRS'
          }
        }
      ] : []
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
// Outputs consumed by scripts / the Rover CLI.
// ---------------------------------------------------------------------------
output vmName string = vm.name
output resourceGroup string = resourceGroup().name
output location string = location
output adminUsername string = adminUsername
output vmSize string = profile.vmSize
output publicIp string = pip.properties.ipAddress
output fqdn string = pip.properties.dnsSettings.fqdn
output privateIp string = nic.properties.ipConfigurations[0].properties.privateIPAddress
output sshTarget string = '${adminUsername}@${pip.properties.dnsSettings.fqdn}'
